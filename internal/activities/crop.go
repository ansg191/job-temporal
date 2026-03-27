package activities

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"math"

	"github.com/ansg191/job-temporal/internal/builder"
)

// US Letter page dimensions in points (1 point = 1/72 inch).
const (
	PageWidthPt     = 612.0 // 8.5 inches
	PageHeightPt    = 792.0 // 11 inches
	MarginPt        = 72.0  // 1 inch on all sides
	ContentWidthPt  = 468.0 // PageWidthPt - 2*MarginPt
	ContentHeightPt = 648.0 // PageHeightPt - 2*MarginPt
)

// PageCropRect pairs a page number with the pixel-space
// rectangle to crop from that page's rendered PNG.
type PageCropRect struct {
	Page int
	Rect image.Rectangle
}

// CropPNG decodes a PNG image from raw bytes, crops it to
// the given rectangle (clamped to image bounds), and
// re-encodes the result as PNG.
func CropPNG(pngData []byte, rect image.Rectangle) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return nil, fmt.Errorf("decode png: %w", err)
	}

	// SubImage is not on the image.Image interface - we
	// need to type-assert to the concrete sub-imager.
	subImage, ok := src.(interface {
		SubImage(r image.Rectangle) image.Image
	})
	if !ok {
		return nil, fmt.Errorf("decoded image does not support subimage cropping")
	}

	// Clamp the crop rect to the actual image bounds so we
	// never panic on out-of-range coordinates.
	clamped := rect.Intersect(src.Bounds())
	if clamped.Empty() {
		return nil, fmt.Errorf("crop rectangle is outside image bounds")
	}

	var out bytes.Buffer
	if err := png.Encode(&out, subImage.SubImage(clamped)); err != nil {
		return nil, fmt.Errorf("encode cropped png: %w", err)
	}

	return out.Bytes(), nil
}

// CropRectsForLabel converts a Typst label bounding box
// (in points) into pixel-space crop rectangles, one per
// page the section occupies.
//
// For single-page sections the result is one rect covering
// the section exactly. For multi-page sections the first
// page is cropped from the section start to the page
// bottom, and subsequent pages use the full content area.
// The total number of pages is capped at maxPages.
//
// The bbox coordinates come from `typst query` and use the
// format "123.45pt". PPI controls the pt-to-px conversion
// (192 PPI = 2.667x scale).
func CropRectsForLabel(bbox builder.LabelBBox, ppi int, maxPages int) ([]PageCropRect, error) {
	if ppi <= 0 {
		return nil, fmt.Errorf("ppi must be positive")
	}
	if maxPages <= 0 {
		return nil, fmt.Errorf("maxPages must be positive")
	}

	// Convert all point-based coordinates to pixels.
	xPx, err := builder.PtToPixels(bbox.Pos.X, ppi)
	if err != nil {
		return nil, fmt.Errorf("convert label x: %w", err)
	}
	yPx, err := builder.PtToPixels(bbox.Pos.Y, ppi)
	if err != nil {
		return nil, fmt.Errorf("convert label y: %w", err)
	}
	wPx, err := builder.PtToPixels(bbox.Size.Width, ppi)
	if err != nil {
		return nil, fmt.Errorf("convert label width: %w", err)
	}
	hPx, err := builder.PtToPixels(bbox.Size.Height, ppi)
	if err != nil {
		return nil, fmt.Errorf("convert label height: %w", err)
	}

	// Pre-compute page-level pixel boundaries.
	// These are the same for every page (US Letter).
	scale := float64(ppi) / builder.PointsPerInch
	pageWidthPx := int(math.Round(PageWidthPt * scale))
	pageHeightPx := int(math.Round(PageHeightPt * scale))
	marginPx := int(math.Round(MarginPt * scale))
	contentHeightPx := int(math.Round(ContentHeightPt * scale))

	// The bottom of the printable area on any page.
	pageBottomPx := int(math.Round((PageHeightPt - MarginPt) * scale))

	// Section bounds in pixels (on the first page).
	sectionTop := int(math.Floor(yPx))
	sectionLeft := int(math.Floor(xPx))
	sectionRight := int(math.Ceil(xPx + wPx))
	sectionBottom := int(math.Ceil(yPx + hPx))

	// --- Single-page case ---
	// The section fits entirely on its starting page if
	// its bottom edge doesn't exceed the page's printable
	// area bottom.
	if yPx+hPx <= float64(pageBottomPx) {
		return []PageCropRect{{
			Page: bbox.Pos.Page,
			Rect: image.Rect(
				sectionLeft, sectionTop,
				sectionRight, sectionBottom,
			),
		}}, nil
	}

	// --- Multi-page case ---
	// The section overflows past the first page. Calculate
	// how many additional pages it spans based on the
	// remaining content height after the first page.
	remainingPx := (yPx + hPx) - float64(pageBottomPx)
	pagesAfterFirst := int(math.Ceil(
		remainingPx / float64(contentHeightPx),
	))
	totalPages := 1 + pagesAfterFirst
	if totalPages > maxPages {
		totalPages = maxPages
	}

	rects := make([]PageCropRect, 0, totalPages)

	// First page: from section start to page bottom.
	rects = append(rects, PageCropRect{
		Page: bbox.Pos.Page,
		Rect: image.Rect(
			sectionLeft, sectionTop,
			sectionRight, pageBottomPx,
		),
	})

	// Subsequent pages: full content area (margin to
	// margin) since the section fills the entire page.
	for i := 1; i < totalPages; i++ {
		rects = append(rects, PageCropRect{
			Page: bbox.Pos.Page + i,
			Rect: image.Rect(
				marginPx, marginPx,
				pageWidthPx-marginPx, pageHeightPx-marginPx,
			),
		})
	}

	return rects, nil
}

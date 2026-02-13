package activities

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"go.temporal.io/sdk/temporal"

	"github.com/ansg191/job-temporal/internal/github"
)

const ErrTypeBuildFailed = "BuildFailed"

type BuildFinalPDFRequest struct {
	github.ClientOptions
	Branch  string `json:"branch"`
	Builder string `json:"builder"`
	File    string `json:"file"`
}

func BuildFinalPDF(ctx context.Context, req BuildFinalPDFRequest) ([]byte, error) {
	tmpFile, err := os.CreateTemp(os.TempDir(), "final-pdf-*.pdf")
	if err != nil {
		return nil, err
	}
	if err = tmpFile.Close(); err != nil {
		return nil, err
	}
	defer os.Remove(tmpFile.Name())

	buildResult, err := runBuild(ctx, req.ClientOptions, req.Branch, req.Builder, req.File, tmpFile.Name())
	if err != nil {
		return nil, err
	}
	if !buildResult.Success {
		return nil, temporal.NewNonRetryableApplicationError(
			"build failed",
			ErrTypeBuildFailed,
			nil,
			buildResult.Errors,
		)
	}

	return os.ReadFile(tmpFile.Name())
}

type UploadPDFRequest struct {
	Content []byte `json:"content"`
}

func UploadPDF(ctx context.Context, req UploadPDFRequest) (string, error) {
	r2cfg, err := loadR2Config()
	if err != nil {
		return "", err
	}

	key := uuid.NewString() + ".pdf"
	if err = uploadBytesToR2(ctx, r2cfg, key, req.Content); err != nil {
		return "", err
	}

	return strings.TrimRight(r2cfg.PublicBaseURL, "/") + "/" + key, nil
}

type DeletePDFByURLRequest struct {
	URL string `json:"url"`
}

func DeletePDFByURL(ctx context.Context, req DeletePDFByURLRequest) error {
	r2cfg, err := loadR2Config()
	if err != nil {
		return err
	}

	key, err := objectKeyFromPublicURL(req.URL)
	if err != nil {
		return err
	}

	s3Client, err := newR2S3Client(ctx, r2cfg.Endpoint)
	if err != nil {
		return err
	}

	_, err = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(r2cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to delete r2 object %q: %w", key, err)
	}
	return nil
}

type r2Config struct {
	Endpoint      string
	Bucket        string
	PublicBaseURL string
}

func loadR2Config() (*r2Config, error) {
	cfg := &r2Config{
		Endpoint:      os.Getenv("AWS_ENDPOINT_URL"),
		Bucket:        os.Getenv("R2_BUCKET"),
		PublicBaseURL: os.Getenv("R2_PUBLIC_BASE_URL"),
	}

	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("missing env var AWS_ENDPOINT_URL")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("missing env var R2_BUCKET")
	}
	if cfg.PublicBaseURL == "" {
		return nil, fmt.Errorf("missing env var R2_PUBLIC_BASE_URL")
	}

	return cfg, nil
}

func uploadBytesToR2(ctx context.Context, cfg *r2Config, key string, content []byte) error {
	return uploadBytesToR2WithContentType(ctx, cfg, key, content, "application/pdf")
}

func uploadBytesToR2WithContentType(ctx context.Context, cfg *r2Config, key string, content []byte, contentType string) error {
	s3Client, err := newR2S3Client(ctx, cfg.Endpoint)
	if err != nil {
		return err
	}

	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(cfg.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(content),
		ContentType: aws.String(contentType),
	})
	return err
}

func newR2S3Client(ctx context.Context, endpoint string) (*s3.Client, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load aws sdk config: %w", err)
	}
	s3Client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = true
	})
	return s3Client, nil
}

func CheckR2ReadWrite(ctx context.Context) error {
	r2cfg, err := loadR2Config()
	if err != nil {
		return err
	}

	s3Client, err := newR2S3Client(ctx, r2cfg.Endpoint)
	if err != nil {
		return err
	}

	key := "startup-check/" + uuid.NewString() + ".txt"
	content := []byte("ok")
	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(r2cfg.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(content),
		ContentType: aws.String("text/plain"),
	})
	if err != nil {
		return fmt.Errorf("r2 write check failed: %w", err)
	}

	getOut, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(r2cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("r2 read check failed: %w", err)
	}
	defer getOut.Body.Close()

	readContent, err := io.ReadAll(getOut.Body)
	if err != nil {
		return fmt.Errorf("r2 read check failed to read body: %w", err)
	}
	if !bytes.Equal(content, readContent) {
		return fmt.Errorf("r2 read check failed: content mismatch")
	}

	_, err = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(r2cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("r2 cleanup check failed: %w", err)
	}

	return nil
}

func objectKeyFromPublicURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid artifact URL: %w", err)
	}

	key := strings.TrimPrefix(u.Path, "/")
	key = path.Clean(key)
	if key == "." || key == "" {
		return "", fmt.Errorf("artifact URL has empty key")
	}
	return key, nil
}

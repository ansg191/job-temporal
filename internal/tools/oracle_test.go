package tools

import "testing"

func TestOracleToolParseArgs_Valid(t *testing.T) {
	t.Parallel()

	var req OracleArgs
	err := OracleToolParseArgs(`{"label":"education","questions":["Does line span 2 lines?","Is spacing even?"]}`, &req)
	if err != nil {
		t.Fatalf("OracleToolParseArgs returned error: %v", err)
	}

	if req.Label != "education" {
		t.Fatalf("Label = %q, want %q", req.Label, "education")
	}
	if len(req.Questions) != 2 {
		t.Fatalf("len(Questions) = %d, want 2", len(req.Questions))
	}
}

func TestOracleToolParseArgs_EmptyLabel(t *testing.T) {
	t.Parallel()

	var req OracleArgs
	err := OracleToolParseArgs(`{"label":"","questions":["question"]}`, &req)
	if err == nil {
		t.Fatal("OracleToolParseArgs returned nil error, want error")
	}
}

func TestOracleToolParseArgs_EmptyQuestions(t *testing.T) {
	t.Parallel()

	var req OracleArgs
	err := OracleToolParseArgs(`{"label":"education","questions":[]}`, &req)
	if err == nil {
		t.Fatal("OracleToolParseArgs returned nil error, want error")
	}
}

func TestOracleToolParseArgs_NoQuestions(t *testing.T) {
	t.Parallel()

	var req OracleArgs
	err := OracleToolParseArgs(`{"label":"education"}`, &req)
	if err == nil {
		t.Fatal("OracleToolParseArgs returned nil error, want error")
	}
}

func TestOracleToolParseArgs_InvalidJSON(t *testing.T) {
	t.Parallel()

	var req OracleArgs
	err := OracleToolParseArgs("not json", &req)
	if err == nil {
		t.Fatal("OracleToolParseArgs returned nil error, want error")
	}
}

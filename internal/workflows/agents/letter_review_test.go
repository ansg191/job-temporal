package agents

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/testsuite"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/config"
	"github.com/ansg191/job-temporal/internal/github"
	"github.com/ansg191/job-temporal/internal/llm"
)

func TestSanitizeLetterReviewIssues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   []activities.ReviewLetterContentIssue
		wantLen int
		checkFn func(t *testing.T, got []activities.ReviewLetterContentIssue)
	}{
		{
			name:    "nil input returns nil",
			input:   nil,
			wantLen: 0,
		},
		{
			name:    "empty input returns nil",
			input:   []activities.ReviewLetterContentIssue{},
			wantLen: 0,
		},
		{
			name: "valid issue passes through",
			input: []activities.ReviewLetterContentIssue{
				{IssueType: "factual_error", Severity: "high", Location: "paragraph 2", Evidence: "wrong date", FixHint: "use 2024"},
			},
			wantLen: 1,
		},
		{
			name: "severity normalized to lowercase",
			input: []activities.ReviewLetterContentIssue{
				{IssueType: "grammar", Severity: "  HIGH ", Location: "para 1", Evidence: "typo", FixHint: "fix"},
			},
			wantLen: 1,
			checkFn: func(t *testing.T, got []activities.ReviewLetterContentIssue) {
				if got[0].Severity != "high" {
					t.Fatalf("expected severity 'high', got %q", got[0].Severity)
				}
			},
		},
		{
			name: "unknown severity defaults to medium",
			input: []activities.ReviewLetterContentIssue{
				{IssueType: "style", Severity: "critical", Location: "intro", Evidence: "wordy", FixHint: "shorten"},
			},
			wantLen: 1,
			checkFn: func(t *testing.T, got []activities.ReviewLetterContentIssue) {
				if got[0].Severity != "medium" {
					t.Fatalf("expected severity 'medium', got %q", got[0].Severity)
				}
			},
		},
		{
			name: "missing issue_type filtered out",
			input: []activities.ReviewLetterContentIssue{
				{IssueType: "", Severity: "low", Location: "para 1", Evidence: "something", FixHint: "fix"},
			},
			wantLen: 0,
		},
		{
			name: "missing evidence filtered out",
			input: []activities.ReviewLetterContentIssue{
				{IssueType: "factual_error", Severity: "high", Location: "para 1", Evidence: "", FixHint: "fix"},
			},
			wantLen: 0,
		},
		{
			name: "missing fix_hint filtered out",
			input: []activities.ReviewLetterContentIssue{
				{IssueType: "factual_error", Severity: "high", Location: "para 1", Evidence: "wrong", FixHint: ""},
			},
			wantLen: 0,
		},
		{
			name: "mixed valid and invalid keeps only valid",
			input: []activities.ReviewLetterContentIssue{
				{IssueType: "factual_error", Severity: "high", Location: "p1", Evidence: "wrong", FixHint: "fix"},
				{IssueType: "", Severity: "low", Location: "p2", Evidence: "nope", FixHint: "x"},
				{IssueType: "grammar", Severity: "low", Location: "p3", Evidence: "ok", FixHint: "y"},
			},
			wantLen: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := sanitizeLetterReviewIssues(tc.input)
			if len(got) != tc.wantLen {
				t.Fatalf("expected %d issues, got %d", tc.wantLen, len(got))
			}
			if tc.checkFn != nil {
				tc.checkFn(t, got)
			}
		})
	}
}

func TestBuildLetterReviewUserPrompt(t *testing.T) {
	t.Parallel()

	prompt := buildLetterReviewUserPrompt("Dear Hiring Manager...", "Software Engineer at Acme")

	if !strings.Contains(prompt, "<cover_letter>") {
		t.Fatal("expected <cover_letter> tag in prompt")
	}
	if !strings.Contains(prompt, "Dear Hiring Manager...") {
		t.Fatal("expected letter content in prompt")
	}
	if !strings.Contains(prompt, "<job_description>") {
		t.Fatal("expected <job_description> tag in prompt")
	}
	if !strings.Contains(prompt, "Software Engineer at Acme") {
		t.Fatal("expected job description in prompt")
	}
}

type LetterReviewWorkflowSuite struct {
	suite.Suite
	testsuite.WorkflowTestSuite
	env *testsuite.TestWorkflowEnvironment
}

func TestLetterReviewWorkflowSuite(t *testing.T) {
	suite.Run(t, new(LetterReviewWorkflowSuite))
}

func (s *LetterReviewWorkflowSuite) SetupTest() {
	s.env = s.NewTestWorkflowEnvironment()
	s.env.RegisterWorkflow(ReviewLetterContentWorkflow)
	s.env.RegisterActivity(activities.GetAgentConfig)
	s.env.RegisterActivity(activities.ReadLetterContent)
	s.env.RegisterActivity(activities.CallAI)
}

func (s *LetterReviewWorkflowSuite) AfterTest(_, _ string) {
	s.env.AssertExpectations(s.T())
}

func (s *LetterReviewWorkflowSuite) mockAgentConfig() {
	s.env.OnActivity(activities.GetAgentConfig, mock.Anything, "letter_review").Return(
		&config.AgentConfig{
			Instructions: "Review the cover letter.",
			Model:        "test-model",
		}, nil,
	)
}

func (s *LetterReviewWorkflowSuite) mockReadLetterContent(content string) {
	s.env.OnActivity(activities.ReadLetterContent, mock.Anything, mock.Anything).Return(content, nil)
}

func (s *LetterReviewWorkflowSuite) TestSuccessNoIssues() {
	s.mockAgentConfig()
	s.mockReadLetterContent("Dear Hiring Manager, I am writing to apply...")

	reviewOutput := activities.ReviewLetterContentOutput{
		Summary: "Letter is well-written",
		Issues:  []activities.ReviewLetterContentIssue{},
	}
	outputJSON, err := json.Marshal(reviewOutput)
	s.NoError(err)

	s.env.OnActivity(activities.CallAI, mock.Anything, mock.Anything).Return(
		&activities.AIResponse{
			OutputText:     string(outputJSON),
			ShouldContinue: false,
			ToolCalls:      nil,
		}, nil,
	)

	s.env.ExecuteWorkflow(ReviewLetterContentWorkflow, activities.ReviewLetterContentRequest{
		ClientOptions: github.ClientOptions{},
		Branch:        "test-branch",
		Job:           "Software Engineer at Acme",
	})

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var resultJSON string
	s.NoError(s.env.GetWorkflowResult(&resultJSON))

	var result activities.ReviewLetterContentOutput
	s.NoError(json.Unmarshal([]byte(resultJSON), &result))
	s.Equal("Letter is well-written", result.Summary)
	s.Empty(result.Issues)
}

func (s *LetterReviewWorkflowSuite) TestSuccessWithIssues() {
	s.mockAgentConfig()
	s.mockReadLetterContent("Dear Hiring Manager, I am writing to apply...")

	reviewOutput := activities.ReviewLetterContentOutput{
		Summary: "Found some issues",
		Issues: []activities.ReviewLetterContentIssue{
			{IssueType: "factual_error", Severity: "high", Location: "paragraph 2", Evidence: "wrong company name", FixHint: "change to Acme"},
			{IssueType: "ai_detected", Severity: "medium", Location: "paragraph 1", Evidence: "generic opener", FixHint: "personalize"},
		},
	}
	outputJSON, err := json.Marshal(reviewOutput)
	s.NoError(err)

	s.env.OnActivity(activities.CallAI, mock.Anything, mock.Anything).Return(
		&activities.AIResponse{
			OutputText:     string(outputJSON),
			ShouldContinue: false,
		}, nil,
	)

	s.env.ExecuteWorkflow(ReviewLetterContentWorkflow, activities.ReviewLetterContentRequest{
		Branch: "test-branch",
		Job:    "Engineer at Acme",
	})

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var resultJSON string
	s.NoError(s.env.GetWorkflowResult(&resultJSON))

	var result activities.ReviewLetterContentOutput
	s.NoError(json.Unmarshal([]byte(resultJSON), &result))
	s.Len(result.Issues, 2)
	s.Equal("high", result.Issues[0].Severity)
}

func (s *LetterReviewWorkflowSuite) TestEmptyLetterContent() {
	s.mockAgentConfig()
	s.mockReadLetterContent("   ")

	s.env.ExecuteWorkflow(ReviewLetterContentWorkflow, activities.ReviewLetterContentRequest{
		Branch: "test-branch",
		Job:    "some job",
	})

	s.True(s.env.IsWorkflowCompleted())
	err := s.env.GetWorkflowError()
	s.Error(err)
	s.Contains(err.Error(), "empty letter content")
}

func (s *LetterReviewWorkflowSuite) TestUnexpectedToolCalls() {
	s.mockAgentConfig()
	s.mockReadLetterContent("Dear Hiring Manager...")

	s.env.OnActivity(activities.CallAI, mock.Anything, mock.Anything).Return(
		&activities.AIResponse{
			OutputText:     "{}",
			ShouldContinue: false,
			ToolCalls:      []llm.ToolCall{{CallID: "call_1", Name: "some_tool"}},
		}, nil,
	)

	s.env.ExecuteWorkflow(ReviewLetterContentWorkflow, activities.ReviewLetterContentRequest{
		Branch: "test-branch",
		Job:    "some job",
	})

	s.True(s.env.IsWorkflowCompleted())
	err := s.env.GetWorkflowError()
	s.Error(err)
	s.Contains(err.Error(), "unexpected tool calls")
}

func (s *LetterReviewWorkflowSuite) TestContinuationLoop() {
	s.mockAgentConfig()
	s.mockReadLetterContent("Dear Hiring Manager...")

	reviewOutput := activities.ReviewLetterContentOutput{
		Summary: "ok",
		Issues:  []activities.ReviewLetterContentIssue{},
	}
	outputJSON, err := json.Marshal(reviewOutput)
	s.NoError(err)

	callCount := 0
	s.env.OnActivity(activities.CallAI, mock.Anything, mock.Anything).Return(
		func(_ context.Context, _ activities.AIRequest) (*activities.AIResponse, error) {
			callCount++
			if callCount == 1 {
				return &activities.AIResponse{ShouldContinue: true}, nil
			}
			return &activities.AIResponse{
				OutputText:     string(outputJSON),
				ShouldContinue: false,
			}, nil
		},
	)

	s.env.ExecuteWorkflow(ReviewLetterContentWorkflow, activities.ReviewLetterContentRequest{
		Branch: "test-branch",
		Job:    "job",
	})

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())
	s.Equal(2, callCount)
}

func (s *LetterReviewWorkflowSuite) TestReadLetterContentActivityError() {
	s.mockAgentConfig()
	s.env.OnActivity(activities.ReadLetterContent, mock.Anything, mock.Anything).Return(
		"", errTestActivity,
	)

	s.env.ExecuteWorkflow(ReviewLetterContentWorkflow, activities.ReviewLetterContentRequest{
		Branch: "test-branch",
		Job:    "some job",
	})

	s.True(s.env.IsWorkflowCompleted())
	s.Error(s.env.GetWorkflowError())
}

func (s *LetterReviewWorkflowSuite) TestInvalidAIOutputJSON() {
	s.mockAgentConfig()
	s.mockReadLetterContent("Dear Hiring Manager...")

	s.env.OnActivity(activities.CallAI, mock.Anything, mock.Anything).Return(
		&activities.AIResponse{
			OutputText:     "this is not json",
			ShouldContinue: false,
		}, nil,
	)

	s.env.ExecuteWorkflow(ReviewLetterContentWorkflow, activities.ReviewLetterContentRequest{
		Branch: "test-branch",
		Job:    "some job",
	})

	s.True(s.env.IsWorkflowCompleted())
	err := s.env.GetWorkflowError()
	s.Error(err)
	s.Contains(err.Error(), "failed to parse letter review output")
}

var errTestActivity = errors.New("activity failed")

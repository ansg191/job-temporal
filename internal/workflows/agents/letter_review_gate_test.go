package agents

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/github"
)

func TestShouldBlockLetterIssuesNilOutput(t *testing.T) {
	t.Parallel()
	blocked, reason := shouldBlockLetterIssues(nil, 1)
	if blocked {
		t.Fatalf("expected no block for nil output, got reason %q", reason)
	}
}

func TestShouldBlockLetterIssues(t *testing.T) {
	t.Parallel()

	issue := func(severity string) activities.ReviewLetterContentIssue {
		return activities.ReviewLetterContentIssue{
			IssueType: "test",
			Severity:  severity,
			Location:  "paragraph 1",
			Evidence:  "found something",
			FixHint:   "fix it",
		}
	}

	tests := []struct {
		name    string
		issues  []activities.ReviewLetterContentIssue
		attempt int
		blocked bool
	}{
		{
			name:    "empty issues never blocks",
			issues:  []activities.ReviewLetterContentIssue{},
			attempt: 1,
			blocked: false,
		},
		{
			name:    "high severity always blocks attempt 1",
			issues:  []activities.ReviewLetterContentIssue{issue("high")},
			attempt: 1,
			blocked: true,
		},
		{
			name:    "high severity always blocks after relaxation",
			issues:  []activities.ReviewLetterContentIssue{issue("high")},
			attempt: 10,
			blocked: true,
		},
		{
			name:    "medium blocks during strict pass attempt 1",
			issues:  []activities.ReviewLetterContentIssue{issue("medium")},
			attempt: 1,
			blocked: true,
		},
		{
			name:    "medium blocks during strict pass attempt 2",
			issues:  []activities.ReviewLetterContentIssue{issue("medium")},
			attempt: reviewGateStrictMediumAttempts,
			blocked: true,
		},
		{
			name:    "single medium passes after relaxation",
			issues:  []activities.ReviewLetterContentIssue{issue("medium")},
			attempt: reviewGateStrictMediumAttempts + 1,
			blocked: false,
		},
		{
			name: "many medium blocks even after relaxation",
			issues: []activities.ReviewLetterContentIssue{
				issue("medium"), issue("medium"), issue("medium"), issue("medium"),
			},
			attempt: reviewGateStrictMediumAttempts + 1,
			blocked: true,
		},
		{
			name:    "exactly max medium passes after relaxation",
			issues:  repeatIssue(issue("medium"), reviewGateMaxMediumAfterRelax),
			attempt: reviewGateStrictMediumAttempts + 1,
			blocked: false,
		},
		{
			name:    "low severity never blocks",
			issues:  []activities.ReviewLetterContentIssue{issue("low"), issue("low")},
			attempt: 1,
			blocked: false,
		},
		{
			name:    "mixed high and low blocks on high",
			issues:  []activities.ReviewLetterContentIssue{issue("low"), issue("high"), issue("low")},
			attempt: 5,
			blocked: true,
		},
		{
			name:    "mixed medium and low blocks during strict",
			issues:  []activities.ReviewLetterContentIssue{issue("low"), issue("medium")},
			attempt: 1,
			blocked: true,
		},
		{
			name:    "mixed medium and low passes after relaxation",
			issues:  []activities.ReviewLetterContentIssue{issue("low"), issue("medium")},
			attempt: reviewGateStrictMediumAttempts + 1,
			blocked: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			output := &activities.ReviewLetterContentOutput{
				Summary: "test summary",
				Issues:  tc.issues,
			}
			blocked, reason := shouldBlockLetterIssues(output, tc.attempt)
			if blocked != tc.blocked {
				t.Fatalf("attempt %d: expected blocked=%v, got blocked=%v reason=%q",
					tc.attempt, tc.blocked, blocked, reason)
			}
		})
	}
}

func repeatIssue(issue activities.ReviewLetterContentIssue, n int) []activities.ReviewLetterContentIssue {
	out := make([]activities.ReviewLetterContentIssue, n)
	for i := range out {
		out[i] = issue
	}
	return out
}

type LetterReviewGateSuite struct {
	suite.Suite
	testsuite.WorkflowTestSuite
	env *testsuite.TestWorkflowEnvironment
}

func TestLetterReviewGateSuite(t *testing.T) {
	suite.Run(t, new(LetterReviewGateSuite))
}

func (s *LetterReviewGateSuite) SetupTest() {
	s.env = s.NewTestWorkflowEnvironment()
}

func (s *LetterReviewGateSuite) AfterTest(_, _ string) {
	s.env.AssertExpectations(s.T())
}

type gateResult struct {
	Output  *activities.ReviewLetterContentOutput
	RawJSON string
}

func gateWrapper(ctx workflow.Context, req activities.ReviewLetterContentRequest) (gateResult, error) {
	output, rawJSON, err := runLetterReviewGate(ctx, "test-child-wf-id", req)
	return gateResult{Output: output, RawJSON: rawJSON}, err
}

func (s *LetterReviewGateSuite) TestSuccessfulReview() {
	expected := activities.ReviewLetterContentOutput{
		Summary: "looks good",
		Issues:  []activities.ReviewLetterContentIssue{},
	}
	jsonBytes, err := json.Marshal(expected)
	s.NoError(err)

	s.env.RegisterWorkflow(gateWrapper)
	s.env.OnWorkflow(ReviewLetterContentWorkflow, mock.Anything, mock.Anything).
		Return(string(jsonBytes), nil)

	s.env.ExecuteWorkflow(gateWrapper, activities.ReviewLetterContentRequest{
		ClientOptions: github.ClientOptions{},
		Branch:        "test-branch",
		Job:           "test job desc",
	})

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())

	var result gateResult
	s.NoError(s.env.GetWorkflowResult(&result))
	s.Equal("looks good", result.Output.Summary)
	s.Equal(string(jsonBytes), result.RawJSON)
}

func (s *LetterReviewGateSuite) TestInvalidJSON() {
	s.env.RegisterWorkflow(gateWrapper)
	s.env.OnWorkflow(ReviewLetterContentWorkflow, mock.Anything, mock.Anything).
		Return("not valid json{{{", nil)

	s.env.ExecuteWorkflow(gateWrapper, activities.ReviewLetterContentRequest{
		Branch: "test-branch",
		Job:    "test job",
	})

	s.True(s.env.IsWorkflowCompleted())
	err := s.env.GetWorkflowError()
	s.Error(err)
	s.Contains(err.Error(), "failed to parse letter review output")
}

func (s *LetterReviewGateSuite) TestChildWorkflowError() {
	s.env.RegisterWorkflow(gateWrapper)
	s.env.OnWorkflow(ReviewLetterContentWorkflow, mock.Anything, mock.Anything).
		Return("", errors.New("child workflow failed"))

	s.env.ExecuteWorkflow(gateWrapper, activities.ReviewLetterContentRequest{
		Branch: "test-branch",
		Job:    "test job",
	})

	s.True(s.env.IsWorkflowCompleted())
	err := s.env.GetWorkflowError()
	s.Error(err)
	s.Contains(err.Error(), "child workflow failed")
}

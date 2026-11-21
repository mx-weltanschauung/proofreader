package models

import (
	"encoding/json"
	"time"
)

type ProofreadRun struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	WorkID    int64     `json:"work_id"`
	ChapterID int64     `json:"chapter_id"`
	CreatedAt time.Time `json:"created_at"`
}

type ProofreadAnalysisStatus string

const (
	ProofreadAnalysisPending  ProofreadAnalysisStatus = "pending"
	ProofreadAnalysisAnalyzed ProofreadAnalysisStatus = "analyzed"
	ProofreadAnalysisFailed   ProofreadAnalysisStatus = "failed"
)

type ProofreadRunPage struct {
	PageID         int64                   `json:"page_id"`
	PageNumber     int                     `json:"page_number"`
	BaseMarkdown   string                  `json:"base_markdown"`
	BaseSHA256     string                  `json:"base_sha256"`
	AnalysisStatus ProofreadAnalysisStatus `json:"analysis_status"`
	AnalysisError  string                  `json:"analysis_error,omitempty"`
	SuggestionID   *int64                  `json:"suggestion_id,omitempty"`
	SubmittedAt    *time.Time              `json:"submitted_at,omitempty"`
	Findings       []ProofreadFinding      `json:"findings"`
}

type FindingDecision string

const (
	FindingPending  FindingDecision = "pending"
	FindingAccepted FindingDecision = "accepted"
	FindingRejected FindingDecision = "rejected"
)

type ProofreadFinding struct {
	ID           int64           `json:"id"`
	CandidateKey string          `json:"candidate_key"`
	StartRune    int             `json:"start_rune"`
	EndRune      int             `json:"end_rune"`
	Old          string          `json:"old"`
	New          string          `json:"new"`
	Source       string          `json:"source"`
	Reason       string          `json:"reason"`
	ScanBBox     json.RawMessage `json:"scan_bbox,omitempty"`
	Decision     FindingDecision `json:"decision"`
	Replacement  string          `json:"replacement"`
}

type PageSubmitResult struct {
	PageID       int64  `json:"page_id"`
	Status       string `json:"status"`
	SuggestionID *int64 `json:"suggestion_id,omitempty"`
}

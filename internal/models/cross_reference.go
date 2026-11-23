package models

const (
	CrossReferencePending   = "pending"
	CrossReferenceConfirmed = "confirmed"
	CrossReferenceRejected  = "rejected"
)

type CrossReferenceOccurrence struct {
	SourcePageID  int64
	SourceDigest  []byte
	Note          string
	Ordinal       int
	Quote         string
	Context       string
	Volume        int
	PrintedPage   int
	EditionNumber int
}

type CrossReferenceLocation struct {
	WorkID      int64  `json:"work_id"`
	PageID      int64  `json:"page_id"`
	PageNumber  int    `json:"page_number"`
	PrintedPage int    `json:"printed_page"`
	WorkTitle   string `json:"work_title"`
	VolumePart  string `json:"volume_part,omitempty"`
	Closed      bool   `json:"closed,omitempty"`
}

type CrossReferenceCandidate struct {
	ID            int64                    `json:"id"`
	Quote         string                   `json:"quote"`
	Context       string                   `json:"context"`
	Volume        int                      `json:"volume"`
	PrintedPage   int                      `json:"printed_page"`
	EditionNumber int                      `json:"edition_number,omitempty"`
	Status        string                   `json:"status"`
	Issue         string                   `json:"issue,omitempty"`
	Source        CrossReferenceLocation   `json:"source"`
	Targets       []CrossReferenceLocation `json:"targets"`
	TargetPageID  *int64                   `json:"target_page_id,omitempty"`
}

// PublishedCrossReference contains the live source anchor and destination.
type PublishedCrossReference struct {
	ID               int64
	SourcePageID     int64
	SourceDigest     []byte
	Note             string
	Ordinal          int
	Quote            string
	TargetPageID     int64
	TargetWorkID     int64
	TargetPageNumber int
}

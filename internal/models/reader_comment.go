package models

import "time"

// CommentViewer создаётся только из проверенной сервером сессии.
type CommentViewer struct {
	ID           int64
	Staff, Admin bool
}

type ReaderComment struct {
	MarginNote
	AuthorNickname    string     `json:"author_nickname"`
	Status            string     `json:"status,omitempty"`
	Revision          int        `json:"revision"`
	PublishedBody     string     `json:"published_body,omitempty"`
	PublishedRevision int        `json:"published_revision,omitempty"`
	RejectReason      string     `json:"reject_reason,omitempty"`
	EditedAt          *time.Time `json:"edited_at,omitempty"`
	CanEdit           bool       `json:"can_edit"`
	CanManage         bool       `json:"can_manage"`
	Rejections        int        `json:"rejections,omitempty"`
	Published         bool       `json:"published"`
	ThreadID          int64      `json:"thread_id,omitempty"`
	ReplyToNickname   *string    `json:"reply_to_nickname,omitempty"`
	ReplyCount        int        `json:"reply_count,omitempty"`
	Removed           string     `json:"removed,omitempty"`
	Unread            bool       `json:"unread,omitempty"`
	RecipientID       *int64     `json:"-"`
	ReplyReadAt       *time.Time `json:"-"`
	AuthorID          *int64     `json:"author_id,omitempty"` // только в очереди сотрудников
}

type CommentThread struct {
	Root    *ReaderComment   `json:"root"`
	Replies []*ReaderComment `json:"replies"`
}

type CommentRevision struct {
	Revision  int       `json:"revision"`
	Body      string    `json:"body"`
	Status    string    `json:"status"`
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type BannedCommenter struct {
	ID       int64  `json:"id"`
	Nickname string `json:"nickname"`
}

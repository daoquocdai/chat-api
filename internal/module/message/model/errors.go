package model

import (
	"errors"
	"fmt"
)

var (
	ErrThreadIDRequired      = errors.New("thread ID is required")
	ErrInvalidThreadID       = errors.New("thread ID must be a UUID")
	ErrMessageIDRequired     = errors.New("message_id is required")
	ErrInvalidMessageID      = errors.New("message_id must be a UUID")
	ErrMessageIDConflict     = errors.New("message_id is already in use")
	ErrEventPublishFailed    = errors.New("message was saved but realtime publish was not confirmed; retry with the same message_id and payload")
	ErrInvalidContent        = errors.New("content must be valid UTF-8, contain between 1 and 1000 Unicode characters, and cannot contain NUL")
	ErrInvalidContentFormat  = errors.New("content_format must be plaintext or e2ee_v1")
	ErrContentFormatConflict = errors.New("content_format does not match thread encryption_mode")
	ErrInvalidEnvelope       = errors.New("invalid e2ee_v1 envelope")
	ErrInvalidEnvelopeHeader = errors.New("e2ee_v1 header does not match the thread participants or registered keys")
	ErrInvalidBeforeSeq      = errors.New("before_seq must be a positive integer")
	ErrInvalidLimit          = errors.New("limit must be an integer between 1 and 100")
)

type EventPublishError struct {
	MessageID string
	ThreadID  string
	SenderID  string
	Seq       int64
	Cause     error
}

func (e *EventPublishError) Error() string {
	return "publish message.created failed" +
		" message_id=" + e.MessageID +
		" thread_id=" + e.ThreadID +
		" sender_id=" + e.SenderID +
		" seq=" + fmt.Sprint(e.Seq) +
		": " + e.Cause.Error()
}

func (e *EventPublishError) Unwrap() []error {
	return []error{ErrEventPublishFailed, e.Cause}
}

const (
	DefaultPageLimit = 30
	MaximumPageLimit = 100
)

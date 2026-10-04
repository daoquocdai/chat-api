package dto

import "github.com/daoquocdai/chat-api/internal/e2ee"

// Only public HTTP wire types belong here, never local private-key DTOs.
type UploadRequest = e2ee.UploadRequest
type UploadResult = e2ee.UploadResult
type ClaimRequest = e2ee.ClaimRequest
type Bundle = e2ee.Bundle

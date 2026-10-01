package handler

import (
	"net/http"

	messagedto "github.com/daoquocdai/chat-api/internal/module/message/dto"
	"github.com/daoquocdai/chat-api/internal/module/thread/dto"
	"github.com/gin-gonic/gin"
)

func (h *Handler) CreateGroup(c *gin.Context) {
	actor, ok := authenticatedUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var request dto.CreateGroupRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	thread, err := h.service.CreateGroup(c.Request.Context(), actor, request.Name, request.MemberIDs)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, dto.ToThreadResponse(thread))
}

func (h *Handler) Members(c *gin.Context) {
	actor, ok := authenticatedUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	members, err := h.service.Members(c.Request.Context(), actor, c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	response := make([]dto.MemberResponse, len(members))
	for i, member := range members {
		response[i] = dto.MemberResponse{ID: member.ExternalID, Username: member.Username, Role: member.Role, JoinedSeq: member.JoinedSeq, LastReadSeq: member.LastReadSeq}
	}
	c.JSON(http.StatusOK, response)
}

func (h *Handler) AddMember(c *gin.Context) {
	actor, ok := authenticatedUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var request dto.AddMemberRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	message, err := h.service.AddMember(c.Request.Context(), actor, c.Param("id"), request.UserID)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, messagedto.ToMessageResponse(message))
}

func (h *Handler) RemoveMember(c *gin.Context) {
	actor, ok := authenticatedUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	message, err := h.service.RemoveMember(c.Request.Context(), actor, c.Param("id"), c.Param("user_id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, messagedto.ToMessageResponse(message))
}

func (h *Handler) Leave(c *gin.Context) {
	actor, ok := authenticatedUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	message, err := h.service.Leave(c.Request.Context(), actor, c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, messagedto.ToMessageResponse(message))
}

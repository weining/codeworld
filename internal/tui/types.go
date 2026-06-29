package tui

import "codeworld/internal/permissions"

type ItemKind string

const (
	ItemUser       ItemKind = "user"
	ItemAssistant  ItemKind = "assistant"
	ItemTool       ItemKind = "tool"
	ItemPermission ItemKind = "permission"
	ItemCommand    ItemKind = "command"
	ItemError      ItemKind = "error"
	ItemNotice     ItemKind = "notice"
)

type TranscriptItem struct {
	Kind ItemKind
	Text string
}

type turnDoneMsg struct {
	text string
	err  error
}

type toolEventMsg struct {
	item TranscriptItem
}

type permissionRequestMsg struct {
	request permissions.Request
}

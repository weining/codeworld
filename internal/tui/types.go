package tui

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

package app

import (
	"fmt"
	"os"

	"codeworld/internal/agent"
	"codeworld/internal/codexplugin"
	"codeworld/internal/config"
	"codeworld/internal/instructions"
	"codeworld/internal/model"
	"codeworld/internal/session"
	"codeworld/internal/skill"
	"codeworld/internal/workspace"
)

// DebugPromptInput 构造模型实际可见的 system、session 与可选用户消息，但不启动 hooks、MCP 或 provider。
func DebugPromptInput(root, profile string, overrides []string, prompt string, images []string) ([]model.Message, error) {
	cfg, err := config.LoadWithOptions(root, config.LoadOptions{Profile: profile, Overrides: overrides})
	if err != nil {
		return nil, err
	}
	configRoot, err := workspace.New(root)
	if err != nil {
		return nil, err
	}
	workspaceRoot := configRoot.Root
	if cfg.Workspace != "" && cfg.Workspace != "." {
		workspaceRoot, err = configRoot.Resolve(cfg.Workspace)
		if err != nil {
			return nil, fmt.Errorf("resolve configured workspace: %w", err)
		}
	}
	ws, err := workspace.New(workspaceRoot)
	if err != nil {
		return nil, err
	}
	summary, err := ws.Summary(120)
	if err != nil {
		summary = "workspace summary unavailable: " + err.Error()
	}
	systemPrompt := agent.DefaultSystemPrompt + "\n\nWorkspace files:\n" + summary
	if indexSummary := loadIndexSummary(ws.Root); indexSummary != "" {
		systemPrompt += "\n\nWorkspace index:\n" + indexSummary
	}
	if graphSummary := loadContextGraphSummary(ws.Root, cfg.IndexMaxFileBytes); graphSummary != "" {
		systemPrompt += "\n\nWorkspace context graph:\n" + graphSummary
	}
	projectInstructions, err := instructions.Load(ws.Root, instructions.Options{MaxBytes: instructions.DefaultMaxBytes})
	if err != nil {
		return nil, err
	}
	if projectInstructions.Text != "" {
		systemPrompt += "\n\nProject instructions:\n" + projectInstructions.Text
	}
	skilled, err := skill.LoadProject(ws.Root)
	if err != nil {
		return nil, err
	}
	projectPlugins, err := codexplugin.LoadProject(ws.Root)
	if err != nil {
		return nil, err
	}
	home, err := config.Home("")
	if err != nil {
		return nil, err
	}
	installedPlugins, err := codexplugin.LoadInstalled(home)
	if err != nil {
		return nil, err
	}
	for _, plugin := range append(projectPlugins, installedPlugins...) {
		skilled = append(skilled, plugin.Skills...)
	}
	if index := skill.Index(skilled); index != "" {
		systemPrompt += "\n\n" + index
	}
	store := session.NewStore(ws.Root)
	sess, err := store.LoadCurrent()
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		sess = session.New(ws.Root, cfg.Provider, cfg.Model)
	}
	history := sanitizeModelMessages(sessionMessagesToModel(sess.Messages, ws))
	messages := []model.Message{{Role: model.RoleSystem, Content: composeSystemPrompt(systemPrompt, sess)}}
	messages = append(messages, history...)
	if prompt != "" || len(images) > 0 {
		user := model.Message{Role: model.RoleUser, Content: prompt}
		if prompt != "" {
			user.Parts = append(user.Parts, model.TextPart(prompt))
		}
		for _, path := range images {
			part, err := model.ImagePartFromFile(path)
			if err != nil {
				return nil, err
			}
			user.Parts = append(user.Parts, part)
		}
		messages = append(messages, user)
	}
	return messages, nil
}

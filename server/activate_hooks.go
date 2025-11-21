package main

import (
	"fmt"
	"strings"

	"github.com/mattermost/mattermost-server/v5/model"
	"github.com/mattermost/mattermost-server/v5/plugin"
	"github.com/pkg/errors"
)

// OnActivate is invoked when the plugin is activated.
//
// This demo implementation logs a message to the demo channel whenever the plugin is activated.
// It also creates a demo bot account
func (p *Plugin) OnActivate() error {
	if err := p.IsValid(); err != nil {
		return err
	}

	if err := p.registerCommands(); err != nil {
		return errors.Wrap(err, "failed to register commands")
	}

	return nil
}

// MessageHasBeenPosted is invoked after a message has been posted.
func (p *Plugin) MessageHasBeenPosted(c *plugin.Context, post *model.Post) {
	configuration := p.getConfiguration()

	// Ignore bot messages to prevent infinite loops
	if post.IsSystemMessage() {
		return
	}

	// Get the user who posted the message
	user, err := p.API.GetUser(post.UserId)
	if err != nil {
		p.API.LogError("Failed to get user", "error", err.Error())
		return
	}

	// Ignore bot posts
	if user.IsBot {
		return
	}

	// Check if the user has auto-translation enabled
	userInfo, apiErr := p.getUserInfo(post.UserId)
	if apiErr != nil {
		// User hasn't configured auto-translate, ignore
		return
	}

	// Check if auto-translation is activated for this user
	if !userInfo.Activated {
		return
	}

	// Get translation provider
	provider, providerErr := p.getTranslationProvider()
	if providerErr != nil {
		p.API.LogError("Failed to get translation provider", "error", providerErr.Error())
		return
	}

	// Perform translation
	translatedText, translateErr := provider.Translate(post.Message, userInfo.SourceLanguage, userInfo.TargetLanguage)
	if translateErr != nil {
		p.API.LogError("Failed to translate message", "error", translateErr.Error())
		return
	}

	// Skip if translation is the same as original (likely same language)
	if strings.TrimSpace(translatedText) == strings.TrimSpace(post.Message) {
		return
	}

	// Get source language display name
	sourceLangDisplay := userInfo.SourceLanguage
	if userInfo.SourceLanguage == "auto" {
		sourceLangDisplay = "detected"
	}

	// Format the translated message with language info
	botMessage := fmt.Sprintf("**[%s → %s]**\n%s",
		sourceLangDisplay,
		userInfo.TargetLanguage,
		translatedText)

	// Create bot username if not configured
	botUsername := configuration.BotUsername
	if botUsername == "" {
		botUsername = "autotranslate-bot"
	}

	// Post the translated message as a reply in the same channel
	translatedPost := &model.Post{
		ChannelId: post.ChannelId,
		UserId:    post.UserId,
		RootId:    post.Id, // Make it a thread reply to the original message
		Message:   botMessage,
		Props: map[string]interface{}{
			"from_plugin":             true,
			"override_username":       botUsername,
			"override_icon_url":       configuration.BotIconURL,
			"disable_group_highlight": true,
		},
	}

	if _, err := p.API.CreatePost(translatedPost); err != nil {
		p.API.LogError("Failed to post translated message", "error", err.Error())
		return
	}
}

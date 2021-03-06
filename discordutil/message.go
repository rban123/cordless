package discordutil

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/Bios-Marcel/discordgo"

	"github.com/Bios-Marcel/cordless/times"
)

// MentionsCurrentUserExplicitly checks whether the message contains any
// explicit mentions for the user associated with the currently logged in user.
func MentionsCurrentUserExplicitly(state *discordgo.State, message *discordgo.Message) bool {
	for _, user := range message.Mentions {
		if user.ID == state.User.ID {
			return true
		}
	}

	return false
}

// MessageDataSupplier defines the method that is necessary for requesting
// channels. This is satisfied by the discordgo.Session struct and can be
// used in order to make testing easier.
type MessageDataSupplier interface {
	ChannelMessages(string, int, string, string, string) ([]*discordgo.Message, error)
}

// MessageLoader represents a util object that remember which channels have
// already been cached and which not.
type MessageLoader struct {
	messageDateSupplier MessageDataSupplier
	requestedChannels   map[string]bool
}

// IsCached checks whether the channel has already been requested from the
// backend once.
func (l *MessageLoader) IsCached(channelID string) bool {
	value, cached := l.requestedChannels[channelID]
	return cached && value
}

func CreateMessageLoader(messageDataSupplier MessageDataSupplier) *MessageLoader {
	loader := &MessageLoader{
		requestedChannels:   make(map[string]bool),
		messageDateSupplier: messageDataSupplier,
	}

	return loader
}

// DeleteFromCache deletes the entry that indicates the channel has been
// cached. The next call to LoadMessages with the same ID will ask for data
// from the MessageDataSupplier.
func (l *MessageLoader) DeleteFromCache(channelID string) {
	delete(l.requestedChannels, channelID)
}

// LoadMessages returns the last 100 messages for a channel. If less messages
// were sent, less will be returned. As soon as a channel has been loaded once
// it won't ever be loaded again, instead a global cache will be accessed.
func (l *MessageLoader) LoadMessages(channel *discordgo.Channel) ([]*discordgo.Message, error) {
	var messages []*discordgo.Message

	if channel.LastMessageID != "" {
		if !l.IsCached(channel.ID) {
			l.requestedChannels[channel.ID] = true

			var beforeID string
			localMessageCount := len(channel.Messages)
			if localMessageCount > 0 {
				beforeID = channel.Messages[0].ID
			}

			messagesToGet := 100 - localMessageCount
			if messagesToGet > 0 {
				var discordError error
				messages, discordError = l.messageDateSupplier.ChannelMessages(channel.ID, messagesToGet, beforeID, "", "")
				if discordError != nil {
					return nil, discordError
				}

				if channel.GuildID != "" {
					for _, message := range messages {
						message.GuildID = channel.GuildID
					}
				}
				if localMessageCount == 0 {
					channel.Messages = messages
				} else {
					//There are already messages in cache; However, those came from updates events.
					//Therefore those have to be newer than the newly retrieved ones.
					channel.Messages = append(messages, channel.Messages...)
				}
			}
		}
		messages = channel.Messages
	}

	return messages, nil
}

// SendMessageAsFile sends the given message into the given channel using the
// passed discord Session. If an error occurs, onFailure gets called.
func SendMessageAsFile(session *discordgo.Session, message string, channel string, onFailure func(error)) {
	reader := bytes.NewBufferString(message)
	messageAsFile := &discordgo.File{
		Name:        "message.txt",
		ContentType: "text",
		Reader:      reader,
	}
	complexMessage := &discordgo.MessageSend{
		Content: "The message was too long, therefore, you get a file:",
		Embed:   nil,
		TTS:     false,
		Files:   nil,
		File:    messageAsFile,
	}
	_, sendError := session.ChannelMessageSendComplex(channel, complexMessage)
	if sendError != nil {
		onFailure(sendError)
	}
}

// Generates a Quote using the given Input. The `messageAfterQuote` will be
// appended after the quote in case it is not empty.
func GenerateQuote(message, author string, time discordgo.Timestamp, attachments []*discordgo.MessageAttachment, messageAfterQuote string) (string, error) {
	messageTime, parseError := time.Parse()
	if parseError != nil {
		return "", parseError
	}

	// All quotes should be UTC.
	messageTimeUTC := messageTime.UTC()

	quotedMessage := strings.ReplaceAll(message, "\n", "\n> ")
	if len(attachments) > 0 {
		var attachmentsAsText string
		for index, attachment := range attachments {
			if index == 0 {
				attachmentsAsText += attachment.URL
			} else {
				attachmentsAsText += "\n> " + attachment.URL
			}
		}

		if strings.HasSuffix(quotedMessage, "> ") {
			quotedMessage = quotedMessage + attachmentsAsText
		} else {
			quotedMessage = quotedMessage + "\n> " + attachmentsAsText
		}
	}
	quotedMessage = fmt.Sprintf("> **%s** %s UTC:\n> %s\n", author, times.TimeToString(&messageTimeUTC), quotedMessage)
	currentContent := strings.TrimSpace(messageAfterQuote)
	if currentContent != "" {
		quotedMessage = quotedMessage + currentContent
	}

	return quotedMessage, nil
}

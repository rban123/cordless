package ui

import (
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
)

type Window struct {
	app              *tview.Application
	messageContainer *tview.Table
	userContainer    *tview.List
	messageInput     *tview.InputField
	channelRootNode  *tview.TreeNode

	session *discordgo.Session

	lastMessageID   *string
	shownMessages   []*discordgo.Message
	selectedServer  *discordgo.UserGuild
	selectedChannel *discordgo.Channel
}

func NewWindow(discord *discordgo.Session) (*Window, error) {
	window := Window{
		session: discord,
	}

	guilds, discordError := discord.UserGuilds(100, "", "")
	if discordError != nil {
		return nil, discordError
	}

	app := tview.NewApplication()

	left := tview.NewPages()

	serversPageName := "Servers"
	serversPage := tview.NewFlex()
	serversPage.SetDirection(tview.FlexRow)

	channelsPlaceholder := tview.NewTreeView()
	channelRootNode := tview.NewTreeNode("")
	window.channelRootNode = channelRootNode
	channelsPlaceholder.SetRoot(channelRootNode)
	channelsPlaceholder.SetBorder(true)
	channelsPlaceholder.SetTopLevel(1)

	serversPlaceholder := tview.NewList()
	serversPlaceholder.SetBorder(true)
	serversPlaceholder.ShowSecondaryText(false)
	for _, guild := range guilds {
		serversPlaceholder.AddItem(guild.Name, "", 0, nil)
	}

	serversPlaceholder.SetSelectedFunc(func(index int, primary, secondary string, shortcut rune) {
		for _, guild := range guilds {
			if guild.Name == primary {
				window.selectedServer = guild
				channelRootNode.ClearChildren()

				//TODO Handle error
				channels, _ := discord.GuildChannels(guild.ID)

				sort.Slice(channels, func(a, b int) bool {
					return channels[a].Position < channels[b].Position
				})

				channelCategories := make(map[string]*tview.TreeNode)
				for _, channel := range channels {
					if channel.ParentID == "" {
						newNode := tview.NewTreeNode(channel.Name)
						channelRootNode.AddChild(newNode)

						if channel.Type == discordgo.ChannelTypeGuildCategory {
							newNode.SetSelectable(false)
							channelCategories[channel.ID] = newNode
						}
					}
				}

				for _, channel := range channels {
					if channel.Type == discordgo.ChannelTypeGuildText && channel.ParentID != "" {
						newNode := tview.NewTreeNode(channel.Name)

						//No selection will prevent selection from working at all.
						if channelsPlaceholder.GetCurrentNode() == nil {
							channelsPlaceholder.SetCurrentNode(newNode)
						}

						newNode.SetSelectable(true)
						//This copy is necessary in order to use the correct channel instead
						//of always the same one.
						channelToConnectTo := channel
						newNode.SetSelectedFunc(func() {
							window.ClearMessages()

							window.selectedChannel = channelToConnectTo
							discordError := window.LoadChannel(channelToConnectTo)
							if discordError != nil {
								log.Fatalf("Error loading messages (%s).", discordError.Error())
							}
						})

						channelCategories[channelToConnectTo.ParentID].AddChild(newNode)
					}
				}

				go func() {
					//TODO Handle error
					window.userContainer.Clear()
					users, _ := discord.GuildMembers(guild.ID, "", 1000)

					app.QueueUpdateDraw(func() {
						for _, user := range users {
							if user.Nick != "" {
								window.userContainer.AddItem(user.Nick, "", 0, nil)
							} else {
								window.userContainer.AddItem(user.User.Username, "", 0, nil)
							}
						}
					})
				}()
				break
			}
		}
	})

	serversPage.AddItem(serversPlaceholder, 0, 1, true)
	serversPage.AddItem(channelsPlaceholder, 0, 2, true)

	left.AddPage(serversPageName, serversPage, true, true)

	friendsPageName := "Friends"
	friendsPage := tview.NewFlex()
	friendsPage.SetDirection(tview.FlexRow)
	left.AddPage(friendsPageName, friendsPage, true, false)

	mid := tview.NewFlex()
	mid.SetDirection(tview.FlexRow)

	messageContainer := tview.NewTable()
	window.messageContainer = messageContainer
	messageContainer.SetBorder(true)
	messageContainer.SetSelectable(true, false)

	messageTick := time.NewTicker(250 * time.Millisecond)
	quitMessageListener := make(chan struct{})
	go func() {
		for {
			select {
			case <-messageTick.C:
				if window.selectedChannel != nil {
					var messages []*discordgo.Message
					var discordError error
					if window.lastMessageID != nil {
						messages, discordError = discord.ChannelMessages(window.selectedChannel.ID, 100, "", *window.lastMessageID, "")
					}

					//TODO Handle properly
					if discordError != nil {
						continue
					}

					if messages == nil || len(messages) == 0 {
						continue
					}

					window.lastMessageID = &messages[len(messages)-1].ID

					window.AddMessages(messages)
				}
			case <-quitMessageListener:
				messageTick.Stop()
				return
			}
		}
	}()

	window.messageInput = tview.NewInputField()
	window.messageInput.SetBorder(true)
	window.messageInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter {
			if window.selectedChannel != nil {
				discord.ChannelMessageSend(window.selectedChannel.ID, window.messageInput.GetText())
				window.messageInput.SetText("")
			}

			return nil
		}

		return event
	})

	mid.AddItem(messageContainer, 0, 1, true)
	mid.AddItem(window.messageInput, 3, 0, true)

	window.userContainer = tview.NewList()
	window.userContainer.ShowSecondaryText(false)
	window.userContainer.SetBorder(true)

	root := tview.NewFlex()
	root.SetDirection(tview.FlexColumn)
	root.SetBorderPadding(-1, -1, 0, 0)

	root.AddItem(left, 0, 7, true)
	root.AddItem(mid, 0, 20, false)
	root.AddItem(window.userContainer, 0, 6, false)

	frame := tview.NewFrame(root)
	frame.SetBorder(true)
	frame.SetTitleAlign(tview.AlignCenter)
	frame.SetTitle("Cordless")

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == 'C' {
			app.SetFocus(channelsPlaceholder)
			return nil
		}

		if event.Rune() == 'S' {
			app.SetFocus(serversPlaceholder)
			return nil
		}

		if event.Rune() == 'T' {
			app.SetFocus(window.messageContainer)
			return nil
		}

		if event.Rune() == 'U' {
			app.SetFocus(window.userContainer)
			return nil
		}

		if event.Rune() == 'M' {
			app.SetFocus(window.messageInput)
			return nil
		}

		return event
	})

	app.SetRoot(frame, true)

	window.app = app

	return &window, nil
}

func (window *Window) ClearMessages() {
	window.messageContainer.Clear()
}

func (window *Window) LoadChannel(channel *discordgo.Channel) error {

	messages, discordError := window.session.ChannelMessages(channel.ID, 100, "", "", "")
	if discordError != nil {
		return discordError
	}

	if messages == nil || len(messages) == 0 {
		return nil
	}

	window.lastMessageID = &messages[0].ID

	//HACK: Reversing them, as they are sorted anyway.
	msgAmount := len(messages)
	for i := 0; i < msgAmount/2; i++ {
		j := msgAmount - i - 1
		messages[i], messages[j] = messages[j], messages[i]
	}

	window.AddMessages(messages)
	return nil
}

func (window *Window) AddMessages(messages []*discordgo.Message) {
	window.shownMessages = append(window.shownMessages, messages...)

	window.app.QueueUpdateDraw(func() {
		for _, message := range messages {
			rowIndex := window.messageContainer.GetRowCount()

			time, parseError := message.Timestamp.Parse()
			if parseError == nil {
				timeCellText := fmt.Sprintf("%02d:%02d:%02d", time.Hour(), time.Minute(), time.Second())
				window.messageContainer.SetCell(rowIndex, 0, tview.NewTableCell(timeCellText))
			}

			//TODO use nickname instead.
			window.messageContainer.SetCell(rowIndex, 1, tview.NewTableCell(message.Author.Username))
			window.messageContainer.SetCell(rowIndex, 2, tview.NewTableCell(message.Content))
		}

		window.messageContainer.Select(window.messageContainer.GetRowCount()-1, 0)
		window.messageContainer.ScrollToEnd()
	})
}

//Run Shows the window optionally returning an error.
func (window *Window) Run() error {
	return window.app.Run()
}

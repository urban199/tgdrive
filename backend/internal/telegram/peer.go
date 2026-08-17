package telegram

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/gotd/td/tg"
)

type PeerStore struct {
	mu     sync.RWMutex
	peer   tg.InputPeerClass
	chatID int64
}

func NewPeerStore(chatID int64) *PeerStore {
	store := &PeerStore{chatID: chatID, peer: peerFromChatID(chatID)}
	return store
}
func (p *PeerStore) Set(peer tg.InputPeerClass) { p.mu.Lock(); p.peer = peer; p.mu.Unlock() }
func (p *PeerStore) Matches(peer tg.InputPeerClass) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.chatID == 0 || peerChatID(peer) == p.chatID
}
func (p *PeerStore) Peer() (tg.InputPeerClass, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.peer, p.peer != nil
}

func CapturePeer(update *tg.UpdateNewMessage, entities tg.Entities) (tg.InputPeerClass, bool) {
	message, ok := update.Message.(*tg.Message)
	if !ok {
		return nil, false
	}
	switch peer := message.PeerID.(type) {
	case *tg.PeerUser:
		user := entities.Users[peer.UserID]
		if user == nil {
			return nil, false
		}
		return &tg.InputPeerUser{UserID: peer.UserID, AccessHash: user.AccessHash}, true
	case *tg.PeerChat:
		return &tg.InputPeerChat{ChatID: peer.ChatID}, true
	case *tg.PeerChannel:
		channel := entities.Channels[peer.ChannelID]
		if channel == nil {
			return nil, false
		}
		return &tg.InputPeerChannel{ChannelID: peer.ChannelID, AccessHash: channel.AccessHash}, true
	default:
		return nil, false
	}
}

func ResolveChatID(ctx context.Context, api *tg.Client, chatID int64) (tg.InputPeerClass, error) {
	if chatID < 0 && chatID > -1000000000000 {
		return &tg.InputPeerChat{ChatID: -chatID}, nil
	}
	if chatID > 0 {
		users, err := api.UsersGetUsers(ctx, []tg.InputUserClass{&tg.InputUser{UserID: chatID}})
		if err != nil {
			return nil, err
		}
		for _, value := range users {
			if user, ok := value.(*tg.User); ok && user.ID == chatID {
				return &tg.InputPeerUser{UserID: user.ID, AccessHash: user.AccessHash}, nil
			}
		}
		return nil, fmt.Errorf("telegram user %d was not found", chatID)
	}
	channelID := -chatID - 1000000000000
	chats, err := api.ChannelsGetChannels(ctx, []tg.InputChannelClass{&tg.InputChannel{ChannelID: channelID}})
	if err != nil {
		return nil, fmt.Errorf("resolve Telegram channel %d: %w", chatID, err)
	}
	switch value := chats.(type) {
	case *tg.MessagesChats:
		for _, item := range value.Chats {
			if channel, ok := item.(*tg.Channel); ok && channel.ID == channelID {
				return &tg.InputPeerChannel{ChannelID: channel.ID, AccessHash: channel.AccessHash}, nil
			}
		}
	case *tg.MessagesChatsSlice:
		for _, item := range value.Chats {
			if channel, ok := item.(*tg.Channel); ok && channel.ID == channelID {
				return &tg.InputPeerChannel{ChannelID: channel.ID, AccessHash: channel.AccessHash}, nil
			}
		}
	}
	return nil, fmt.Errorf("telegram channel %d was not found", chatID)
}

func peerFromChatID(chatID int64) tg.InputPeerClass {
	if chatID > 0 {
		return &tg.InputPeerUser{UserID: chatID}
	}
	if chatID > -1000000000000 {
		return &tg.InputPeerChat{ChatID: -chatID}
	}
	return &tg.InputPeerChannel{ChannelID: -chatID - 1000000000000}
}

func peerChatID(peer tg.InputPeerClass) int64 {
	switch value := peer.(type) {
	case *tg.InputPeerUser:
		return value.UserID
	case *tg.InputPeerChat:
		return -value.ChatID
	case *tg.InputPeerChannel:
		return channelChatID(value.ChannelID)
	default:
		return 0
	}
}

func channelChatID(channelID int64) int64 { return -1000000000000 - channelID }
func ParseChatID(value string) (int64, error) {
	value = strings.TrimSpace(value)
	var id int64
	_, err := fmt.Sscan(value, &id)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("invalid chat id %q", value)
	}
	return id, nil
}

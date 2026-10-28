package network

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"

	"tacc-node/types"
)

type P2PNode struct {
	Host      host.Host
	PubSub    *pubsub.PubSub
	Topic     *pubsub.Topic
	Sub       *pubsub.Subscription
	Inbox     chan types.NetMessage
	PeerSet   map[string]struct{}
	PeerSetMu sync.RWMutex
}

func NewP2PNode(port int) *P2PNode {
	ctx := context.Background()
	addr := fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port)

	h, err := libp2p.New(libp2p.ListenAddrStrings(addr))
	if err != nil {
		log.Fatalf("P2P init error: %v", err)
	}

	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		log.Fatalf("pubsub init error: %v", err)
	}

	topic, err := ps.Join("tacc-events")
	if err != nil {
		log.Fatalf("topic join error: %v", err)
	}

	sub, err := topic.Subscribe()
	if err != nil {
		log.Fatalf("topic subscribe error: %v", err)
	}

	n := &P2PNode{
		Host:    h,
		PubSub:  ps,
		Topic:   topic,
		Sub:     sub,
		Inbox:   make(chan types.NetMessage, 256),
		PeerSet: map[string]struct{}{},
	}

	go n.listen(ctx)

	log.Printf("[P2P] Address: /ip4/127.0.0.1/tcp/%d/p2p/%s", port, h.ID())
	return n
}

func (n *P2PNode) Connect(target string) {
	ma, err := multiaddr.NewMultiaddr(target)
	if err != nil {
		log.Printf("invalid multiaddr: %v", err)
		return
	}

	info, err := peer.AddrInfoFromP2pAddr(ma)
	if err != nil {
		log.Printf("peer info error: %v", err)
		return
	}

	if err := n.Host.Connect(context.Background(), *info); err != nil {
		log.Printf("connect failed: %v", err)
		return
	}

	n.PeerSetMu.Lock()
	n.PeerSet[info.ID.String()] = struct{}{}
	n.PeerSetMu.Unlock()

	log.Printf("[P2P] Connected to peer: %s", info.ID)
}

func (n *P2PNode) PeerCount() int {
	n.PeerSetMu.RLock()
	defer n.PeerSetMu.RUnlock()
	return len(n.PeerSet)
}

func (n *P2PNode) Publish(msg types.NetMessage) {
	b, err := json.Marshal(msg)
	if err != nil {
		log.Printf("publish marshal error: %v", err)
		return
	}

	if err := n.Topic.Publish(context.Background(), b); err != nil {
		log.Printf("publish error: %v", err)
	}
}

func (n *P2PNode) listen(ctx context.Context) {
	for {
		m, err := n.Sub.Next(ctx)
		if err != nil {
			log.Printf("subscription closed: %v", err)
			return
		}

		if m.ReceivedFrom == n.Host.ID() {
			continue
		}

		n.PeerSetMu.Lock()
		n.PeerSet[m.ReceivedFrom.String()] = struct{}{}
		n.PeerSetMu.Unlock()

		var msg types.NetMessage
		if err := json.Unmarshal(m.Data, &msg); err != nil {
			log.Printf("failed to decode message: %v", err)
			continue
		}

		select {
		case n.Inbox <- msg:
		default:
			log.Printf("inbox full, dropping message from %s", m.ReceivedFrom.String())
		}
	}
}

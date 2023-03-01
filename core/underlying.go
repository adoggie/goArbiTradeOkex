package core

import (
	"fmt"
	"github.com/amir-the-h/okex/events/public"
	"github.com/amir-the-h/okex/models/market"
	"github.com/amir-the-h/okex/models/publicdata"
	"sync"
)

type UnderlyingType int

const (
	UnderlyingSPOT UnderlyingType = iota
	UnderlyingSWAP
)

type Underlying struct {
	Name       string
	Type       UnderlyingType
	LastPrice  float64
	book       *public.OrderBook
	Peer       *Underlying
	BrokerId   string
	Pair       *UnderlyingPair
	Instrument *publicdata.Instrument
	mtx        sync.RWMutex
}

func (uly *Underlying) SetOrderBook(orb *public.OrderBook) {

	uly.mtx.Lock()
	defer uly.mtx.Unlock()
	uly.book = orb
}

func (uly *Underlying) GetOrderBook() *public.OrderBook {

	uly.mtx.Lock()
	defer uly.mtx.Unlock()
	return uly.book
}

func (uly *Underlying) GetOrderBookAsk(n int) *market.OrderBookEntity {
	return uly.GetOrderBook().Books[0].Asks[n]
}

func (uly *Underlying) GetOrderBookAskLast() *market.OrderBookEntity {
	TAIL := len(uly.GetOrderBook().Books[0].Asks) - 1
	return uly.GetOrderBookAsk(TAIL)
}

func (uly *Underlying) GetOrderBookBid(n int) *market.OrderBookEntity {
	return uly.GetOrderBook().Books[0].Bids[n]
}

type UnderlyingPair struct {
	//Spot   string `json:"spot,omitempty"`
	//Swap   string `json:"swap,omitempty"`
	//Enable bool   `json:"enable,omitempty"`
	Spot *Underlying
	Swap *Underlying
}

func (pair UnderlyingPair) InPair(name string) bool {
	if pair.Spot.Name == name || pair.Swap.Name == name {
		return true
	}
	return false
}

func (pair *UnderlyingPair) Id() string {
	id := fmt.Sprintf("%s#%s", pair.Spot.Name, pair.Swap.Name)
	return id
}

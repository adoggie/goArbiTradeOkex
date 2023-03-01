package core

import (
	"context"
	"math"
	"sync"
	"time"
)

type StrategyConfigVars struct {
	ID          string         `json:"id,omitempty"`
	EnterValue  float64        // 进场利润
	LeaveValue  float64        // 离场利润
	Underlyings []*SpotAndSwap `json:"underlyings,omitempty"` // 策略交易对清单
}

type Strategy struct {
	*CommBase
	chSig chan Signal
	//chOrb chan *public.OrderBookEvent
	users           []SignalUser
	mtx             sync.RWMutex
	config          *StrategyConfigVars
	underlyings     map[string]*Underlying
	underlyingPairs []*UnderlyingPair
	ctx             context.Context
	fxExit          context.CancelFunc
	broker          *Broker
}

func (s *Strategy) Name() string {
	return "no name"
}

func (s *Strategy) Init(config *StrategyConfigVars) bool {
	s.config = config

	s.underlyings = make(map[string]*Underlying)
	for _, ss := range s.config.Underlyings {
		ulySpot := s.broker.GetUnderlying(ss.Spot)
		ulySwap := s.broker.GetUnderlying(ss.Swap)
		ulySpot.Peer = ulySwap
		ulySwap.Peer = ulySpot
		s.underlyings[ss.Spot] = ulySpot
		s.underlyings[ss.Swap] = ulySwap
		pair := &UnderlyingPair{Spot: ulySpot, Swap: ulySwap}
		s.underlyingPairs = append(s.underlyingPairs, pair)
	}
	s.ctx, s.fxExit = context.WithCancel(context.Background())
	return true
}

func (s *Strategy) prepare() {

}

func (s *Strategy) Run(ctx context.Context) {
	s.ctx = ctx
	s.prepare()
	go func() {
		for {
			select {
			case <-s.ctx.Done():
				break
			}
		}
	}()

}

func (s *Strategy) AddUser(user SignalUser) {
	s.users = append(s.users, user)
}

func (s *Strategy) OnOrderBook(ev *OrderBookEvent) {
	return

	if _, ok := s.underlyings[ev.Underlying.Name]; !ok {
		return //  not in my list
	}
	var underly *Underlying = ev.Underlying

	//计算价差，并产生信号
	if underly.Peer == nil {
		return // peer OrderBookEvent absent
	}
	underly = ev.Underlying
	N := len(underly.GetOrderBook().Books)
	orb_a := underly.GetOrderBook().Books[0]      // ask[0]
	orb_b := underly.Peer.GetOrderBook().Books[0] // bid[0]

	a := orb_a.Asks[N]
	b := orb_b.Bids[0]

	diff := math.Abs(a.DepthPrice - b.DepthPrice)
	// 计算 报单成本
	var underly_spot, underly_swap *Underlying
	underly_swap = underly
	if underly.Type == UnderlyingSPOT {
		underly_spot = underly
	}
	// 查询报单成交手续费
	tak, mak := s.broker.GetCommission(underly.Name)

	cost := a.DepthPrice * (mak + tak) * 2

	wpc := (diff - cost) / a.DepthPrice

	pair := &UnderlyingPair{
		Spot: underly_spot,
		Swap: underly_swap,
	}
	sig := &Signal{
		Pair:        pair,
		Strategy:    s,
		Action:      Enter,
		ForwardType: ForwardPremium,
		Create:      time.Now(),
	}
	if wpc < s.config.LeaveValue {
		sig.Action = Leave
	}
	if wpc > s.config.EnterValue || wpc < s.config.LeaveValue {
		// 满足条件 ， go signal
		s.EmitSignal(sig)
	}

}

func (s *Strategy) OnOrderReturn(order *OrderEvent) {

}

func (s *Strategy) OnAccount(acc *AccountEvent) {

}

func (s *Strategy) OnPosition(pos *PositionEvent) {

}

func (s *Strategy) OnBalancePosition(balpos *BalanceAndPositionEvent) {

}

func (s *Strategy) OnBrokerEvent(ev *BrokerEvent) {
	if ev.Reason == "WsNetDisconnected" {

	}
}

func (s *Strategy) IsSignalValid(signal *Signal) bool {
	return true
}

func (s *Strategy) EmitSignal(sig *Signal) {
	for _, user := range s.users {
		go func(user SignalUser) {
			user.OnSignal(sig)
		}(user)

	}
}

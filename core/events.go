package core

import (
	"github.com/amir-the-h/okex/events/private"
	"github.com/amir-the-h/okex/events/public"
)

type (
	BrokerEvent struct {
		Reason string
		Broker *Broker
		Data   any
	} // 网络断开连接
	// WsNetDisconnected
	// WsNetConnected

	OrderBookEvent struct {
		Underlying *Underlying
		Inner      *public.OrderBook
		//Broker *core.Broker
	}
	TaskEvent struct {
		Reason string
		//Task   *Task
	}

	OrderEvent struct {
		Inner      *private.Order
		Underlying *Underlying
	}

	AccountEvent struct {
		Inner      *private.Account
		Underlying *Underlying
	}

	PositionEvent struct {
		Inner      *private.Position
		Underlying *Underlying
	}

	BalanceAndPositionEvent struct {
		Inner      *private.BalanceAndPosition
		Underlying *Underlying
	}

	OrderTaskEvent struct {
		Reason string
		Task   *OrderTask
	}
)

//const WsNetDisconnected = BrokerEvent("WsNetDisconnected")

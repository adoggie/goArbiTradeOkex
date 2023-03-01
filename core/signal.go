package core

import (
	"fmt"
	"strconv"
	"time"
)

//策略信号产生服务模块

type Action int
type Direction int
type OpenClose int
type BuySell int
type ForwardType int

func (action Action) String() string {
	return strconv.FormatInt(int64(action), 10)
}

const (
	Enter Action = iota // 进场开仓
	Leave               // 离场平仓
)
const (
	Long  Direction = iota // 多
	Short                  // 空
)
const (
	Open  OpenClose = iota // 开
	Close                  //平
)
const (
	ForwardDiscount ForwardType = iota // 贴水
	ForwardPremium                     // 升水
)

const (
	Buy BuySell = iota
	Sell
)

type Signal struct {
	Pair        *UnderlyingPair
	Action      Action      // 进场或离场
	ForwardType ForwardType // 升水贴水
	Create      time.Time
	Strategy    *Strategy
}

func (s *Signal) Id() string {
	id := fmt.Sprintf("%s_%s", s.Pair.Id(), s.Action.String())
	return id
}

type SignalUser interface {
	OnSignal(signal *Signal)
}

package gotrader

import (
	"math"
	"sync"
	"time"

	"github.com/uber-go/atomic"

	"github.com/sirupsen/logrus"

	"github.com/cornelk/hashmap"
)

// Hedge represents the type of hedging defined by the broker.
type Hedge int

const (
	FullHedge Hedge = iota
	NoHedge
	HalfHedge
)

type Instrument struct {
	name                      string
	baseCurrency              string
	quoteCurrency             string
	longPosition              *Position
	shortPosition             *Position
	trades                    *hashmap.HashMap
	tradesTimeOrder           *sortedTrades
	unrealizedNetProfit       float64
	unrealizedEffectiveProfit float64
	marginUsed                float64
	leverage                  *atomic.Float64
	chargedFees               float64
	ask                       *atomic.Float64
	bid                       *atomic.Float64
	ccyConversion             *instrumentConversion
	hedgeType                 Hedge
	lock                      *sync.RWMutex
}

/**************************
*
*	Internal Methods
*
***************************/

func newInstrument(name, baseCurrency, quoteCurrency string,
	leverage float64) *Instrument {

	return &Instrument{
		name:            name,
		baseCurrency:    baseCurrency,
		quoteCurrency:   quoteCurrency,
		leverage:        atomic.NewFloat64(leverage),
		longPosition:    newPosition(Long, &leverage),
		shortPosition:   newPosition(Short, &leverage),
		trades:          &hashmap.HashMap{},
		tradesTimeOrder: newSortedTrades(),
		ask:             atomic.NewFloat64(0.0),
		bid:             atomic.NewFloat64(0.0),
		lock:            &sync.RWMutex{},
	}
}

func (i *Instrument) openTrade(id string, side Side, openTime time.Time, units int32, openPrice float64) {

	trade := newTrade(id, i.name, side, units, openTime, openPrice, i.ccyConversion)
	i.trades.Set(id, trade)
	i.tradesTimeOrder.Append(id)
	trade.leverage = i.leverage

	if side == Short {
		trade.currentPrice = i.ask
		i.shortPosition.openTrade(trade)
	} else {
		trade.currentPrice = i.bid
		i.longPosition.openTrade(trade)
	}

}

func (i *Instrument) closeTrade(id string) {

	i.tradesTimeOrder.Delete(id)
	tr, exist := i.trades.Get(id)

	if !exist {
		logrus.Warn(i.name + ": trying to close unexisting trade")
		return
	}
	i.trades.Del(id)

	trade := tr.(*Trade)

	if trade.side == Long {
		i.longPosition.closeTrade(trade)
	} else {
		i.shortPosition.closeTrade(trade)
	}

}

func (i *Instrument) calculateUnrealized() {

	i.shortPosition.calculateUnrealized()
	i.longPosition.calculateUnrealized()

	i.unrealizedNetProfit = i.longPosition.unrealizedNetProfit + i.shortPosition.unrealizedNetProfit
	i.unrealizedEffectiveProfit = i.longPosition.unrealizedEffectiveProfit + i.shortPosition.unrealizedEffectiveProfit
	i.chargedFees = i.longPosition.chargedFees + i.shortPosition.chargedFees

}

func (i *Instrument) calculateMarginUsed() {

	i.shortPosition.calculateMarginUsed()
	i.longPosition.calculateMarginUsed()

	switch i.hedgeType {
	case NoHedge:
		i.marginUsed = i.shortPosition.marginUsed + i.longPosition.marginUsed
	case FullHedge:
		i.marginUsed = math.Abs(i.shortPosition.marginUsed - i.longPosition.marginUsed)
	case HalfHedge:
		if i.shortPosition.marginUsed > i.longPosition.marginUsed {
			i.marginUsed = i.shortPosition.marginUsed
		} else {
			i.marginUsed = i.longPosition.marginUsed
		}
	}
}

func (i *Instrument) updatePrice(tick *Tick) {
	i.ask.Store(tick.Ask)
	i.bid.Store(tick.Bid)
}

/**************************
*
*	Acessible Methods
*
***************************/

func (i *Instrument) Name() string {
	return i.name
}

func (i *Instrument) BaseCurrency() string {
	return i.baseCurrency
}

func (i *Instrument) QuoteCurrency() string {
	return i.quoteCurrency
}

func (i *Instrument) LongPosition() *Position {
	return i.longPosition
}

func (i *Instrument) ShortPosition() *Position {
	return i.shortPosition
}

func (i *Instrument) TradeByOrder(index int) string {
	return i.tradesTimeOrder.Get(index)
}

func (i *Instrument) TradesByAscendingOrder(tradesNumber int) <-chan string {
	return i.tradesTimeOrder.AscendIter(tradesNumber)
}

func (i *Instrument) TradesByDescendingOrder(tradesNumber int) <-chan string {
	return i.tradesTimeOrder.DescendIter(tradesNumber)
}

func (i *Instrument) Trade(id string) *Trade {

	trade, exist := i.trades.Get(id)
	if exist {
		return trade.(*Trade)
	}

	return nil
}

func (i *Instrument) Trades() <-chan *Trade {

	ch := make(chan *Trade)
	go func() {
		for kv := range i.trades.Iter() {
			ch <- kv.Value.(*Trade)
		}
		close(ch)
	}()

	return ch
}

func (i *Instrument) UnrealizedNetProfit() float64 {
	return i.unrealizedNetProfit
}

func (i *Instrument) UnrealizedEffectiveProfit() float64 { // = UnrealizedNetProfit + ChargedFees
	return i.unrealizedEffectiveProfit
}

func (i *Instrument) MarginUsed() float64 {
	return i.marginUsed
}

func (i *Instrument) ChargedFees() float64 {
	return i.chargedFees
}

func (i *Instrument) Ask() float64 {
	return i.ask.Load()
}

func (i *Instrument) Bid() float64 {
	return i.bid.Load()
}
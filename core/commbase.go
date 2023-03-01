package core

import "log"

type CommBase struct {
	logger *log.Logger
	broker *Broker
}

func (cb *CommBase) SetLogger(logger *log.Logger) *CommBase {
	cb.logger = logger
	return cb
}

func (cb *CommBase) GetLogger() (logger *log.Logger) {
	logger = cb.logger
	return
}

func (cb *CommBase) SetBroker(broker *Broker) *CommBase {
	cb.broker = broker
	return cb
}

func (cb *CommBase) GetBroker() *Broker {
	return cb.broker
}

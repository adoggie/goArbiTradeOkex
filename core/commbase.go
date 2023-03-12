package core

import (
	"github.com/adoggie/jaguar/utils"
)

type CommBase struct {
	logger     *utils.Logger
	broker     *Broker
	controller *Controller
}

func (cb *CommBase) SetLogger(logger *utils.Logger) *CommBase {
	cb.logger = logger
	return cb
}

func (cb *CommBase) GetLogger() (logger *utils.Logger) {
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

func (cb *CommBase) GetController() *Controller {
	return cb.controller
}

func (cb *CommBase) SetController(controller *Controller) {
	cb.controller = controller
}

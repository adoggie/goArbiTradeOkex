package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/adoggie/jaguar/core"
	"github.com/adoggie/jaguar/utils"
	"io"
	"io/ioutil"
	"log"
	//thislog "logging"
	"os"
	"time"
)

type LoggerVars struct {
	File  string `json:"file,omitempty"`
	Mx    string `json:"mx,omitempty"`
	Name  string `json:"name,omitempty"`
	Level string `json:"level"`
}

type GlobalConfigVars struct {
	Keys         []core.BrokerApiKey         `json:"keys"`
	Broker       core.BrokerConfigVars       `json:"broker"`
	Strategy     core.StrategyConfigVars     `json:"strategy"`
	OrderManager core.OrderManagerConfigVars `json:"orderManager"`
	LoggerVars   LoggerVars                  `json:"logger"`
	Controller   core.ControllerConfigVars   `json:"controller"`
	RiskManager  core.RiskManagerVars        `json:"riskManager"`
}

var (
	//logger *log.Logger
	logger *utils.Logger
)

func initLogger(vars LoggerVars) *utils.Logger {

	fn := vars.File
	format := "2006-01-02"
	t := time.Now().UTC().Format(format)
	fn = fmt.Sprintf("%s_%s.log", fn, t)
	writers := []io.Writer{os.Stdout}
	fp, _ := os.OpenFile(fn, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	writers = append(writers, fp)

	logger = utils.NewLogger(vars.Name, io.MultiWriter(writers...), utils.DEBUG, log.LstdFlags|log.Lshortfile).SetLevelName(vars.Level)

	return logger
}

func main() {
	fn := fmt.Sprintf("%s/settings.json", utils.ExecPath())
	var fp *os.File
	var err error
	var globalvars GlobalConfigVars

	if fp, err = os.Open(fn); err != nil {
		log.Fatalln(err.Error())

	}
	data, _ := ioutil.ReadAll(fp)
	if err := json.Unmarshal(data, &globalvars); err != nil {
		log.Fatalln(err.Error())
	}

	initLogger(globalvars.LoggerVars)

	keyId := globalvars.Broker.Key
	for n, key := range globalvars.Keys {
		if key.Id == keyId {
			globalvars.Broker.ApiKey = &globalvars.Keys[n]
			//_ = n
		}
	}
	if globalvars.Broker.ApiKey == nil {
		logger.Fatalln("Broker's apikey not filled!")
	}

	ctx := context.Background()
	commbase := &core.CommBase{}
	broker := &core.Broker{CommBase: commbase}
	commbase.SetBroker(broker).SetLogger(logger)
	broker.Init(&globalvars.Broker)

	riskMgr := &core.RiskManager{CommBase: commbase}
	riskMgr.Init(&globalvars.RiskManager)

	orderMgr := &core.OrderManager{CommBase: commbase}
	orderMgr.Init(&globalvars.OrderManager)

	strategy := &core.Strategy{CommBase: commbase}
	strategy.Init(&globalvars.Strategy)
	strategy.AddUser(riskMgr)

	broker.AddSubscribers(riskMgr, strategy, orderMgr)

	controller := core.Controller{CommBase: commbase}
	controller.Init(&globalvars.Controller)
	controller.AddStrategy(strategy).SetRiskManager(riskMgr).SetOrderManager(orderMgr)
	commbase.SetController(&controller)
	controller.Start(ctx)

}

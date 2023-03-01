
2023.2.28
1.报单超时不成交检查，重新撤单发单
2.USD 报单 ，第二腿报单测试
3.检查每笔报单成交的orderbook，order信息
4.报单任务结束之后的任务统计输出 
5.盘后偏离撤单重发


2023.2.25
1. okex.com 网站从hk无法访问 ，妈的 搞了我半天时间
2. okex的go包存在bug ， 认证Auth消息不返回会卡死调用线程

### 撤单事件

```json
{
  "arg": {},
  "data": [
    {
      "instId": "SOL-USDT",
      "ccy": "",
      "ordId": "549988717147029504",
      "clOrdId": "",
      "tradeId": "",
      "tag": "",
      "category": "normal",
      "feeCcy": "SOL",
      "rebateCcy": "USDT",
      "px": 20,
      "sz": 1,
      "pnl": 0,
      "accFillSz": 0,
      "fillPx": 0,
      "fillSz": 0,
      "fillTime": 0,
      "avgPx": 0,
      "lever": 0,
      "tpTriggerPx": 0,
      "tpOrdPx": 0,
      "slTriggerPx": 0,
      "slOrdPx": 0,
      "fee": 0,
      "rebate": 0,
      "state": "canceled",
      "tdMode": "cash",
      "posSide": "",
      "side": "buy",
      "ordType": "limit",
      "instType": "SPOT",
      "tgtCcy": "",
      "uTime": {},
      "cTime": {}
    }
  ]
}
```

#curl -v -X POST -H "Content-Type: application/json" http://127.0.0.1:9904/cmd -d '{  "message": "create_order",  "data": {    "spot": "SOL-USD,buy",    "swap": "SOL-USD-SWAP,sell",    "amount": 10  }}'
curl -v -X POST -H "Content-Type: application/json" -d @create_order.json http://127.0.0.1:9904/cmd

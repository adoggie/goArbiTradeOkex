
import os,time,traceback,datetime
import json
import zmq
import fire
import requests

def start_order(spot,swap,amount):
    ctx = zmq.Context()
    sock = ctx.socket(zmq.PUB)
    # sock.connect("ipc://arbiCommand")
    sock.connect("tcp://127.0.0.1:9903")
    data = dict(
        message= "create_order",
        data = dict(
            spot = spot ,
            swap = swap,
            amount = amount
        )
    )
    time.sleep(.1)
    sock.send(json.dumps(data).encode())
    time.sleep(.1)
    



if __name__ == '__main__':
    start_order("SOL-USD,buy","SOL-USD-SWAP,sell",10)
    # fire.Fire()
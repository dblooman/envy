#!/usr/bin/env python3
import argparse
import json
from pathlib import Path
from acceptance import Client

parser = argparse.ArgumentParser(description="Explicitly validate or register the borrowed LAN baseline through REST")
parser.add_argument("action", choices=["validate", "apply"])
parser.add_argument("--api", default="http://192.168.1.172:30081")
parser.add_argument("--ingress", default="http://192.168.1.172:30080")
parser.add_argument("--token-file", required=True)
parser.add_argument("--catalog", default=".envy/lan/catalog.json")
args = parser.parse_args()
client = Client(args.api, args.ingress, Path(args.token_file).read_text().strip())
print(json.dumps(client.api_call("POST", "/v1/catalog/" + args.action, json.loads(Path(args.catalog).read_text())), indent=2))

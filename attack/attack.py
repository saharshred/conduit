#!/usr/bin/env python3
"""Simulate a card-testing / fraud-ring pattern against the live pipeline.

Publishes directly onto the same RabbitMQ exchange the Go normalizer uses
(see internal/queue/queue.go), so the burst flows through the real
ingestor and lands in Postgres exactly like genuine bank traffic would.
Run services/scorer afterward and the burst should show up flagged by the
velocity rule (many transactions, one account, tight window) — that's the
actual point: measure whether your own scorer catches what you built to
attack it.

Usage:
    ./venv/bin/python attack.py --account acct-attack-1 --count 20
"""
import argparse
import json
import time
import uuid
from datetime import datetime, timezone

import pika

EXCHANGE = "conduit"
ROUTING_KEY = "transactions"


def build_transaction(account_id: str, seq: int) -> dict:
    # Deliberately shaped to trip both rules at once: a merchant category
    # ("7995" — gambling) this account has presumably never used, fired
    # off in a tight burst that trips the velocity check too.
    return {
        "bank": "attack-sim",
        "native_id": f"attack-{uuid.uuid4().hex[:10]}-{seq}",
        "account_id": account_id,
        "amount_cents": 500 + (seq * 37) % 4000,
        "currency": "USD",
        "merchant_name": "Unknown Merchant",
        "merchant_category": "7995",
        "city": "New York",
        "timestamp": datetime.now(timezone.utc).isoformat(),
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--amqp-url", default="amqp://guest:guest@localhost:5673/")
    parser.add_argument("--account", default="acct-attack-1", help="account ID to target")
    parser.add_argument("--count", type=int, default=20, help="number of burst transactions")
    parser.add_argument("--interval", type=float, default=0.3, help="seconds between publishes")
    args = parser.parse_args()

    conn = pika.BlockingConnection(pika.URLParameters(args.amqp_url))
    ch = conn.channel()
    ch.exchange_declare(exchange=EXCHANGE, exchange_type="direct", durable=True)

    print(f"attack: publishing {args.count} burst transactions to account {args.account}")
    start = time.time()
    for i in range(args.count):
        txn = build_transaction(args.account, i)
        ch.basic_publish(
            exchange=EXCHANGE,
            routing_key=ROUTING_KEY,
            body=json.dumps(txn).encode(),
            properties=pika.BasicProperties(
                content_type="application/json",
                headers={"x-conduit-retry-count": 0},
                message_id=f"{txn['bank']}:{txn['native_id']}",
            ),
        )
        time.sleep(args.interval)

    elapsed = time.time() - start
    print(f"attack: done — {args.count} transactions in {elapsed:.1f}s "
          f"({args.count / elapsed:.1f}/s). Run the scorer next to see if it caught this.")
    conn.close()


if __name__ == "__main__":
    main()

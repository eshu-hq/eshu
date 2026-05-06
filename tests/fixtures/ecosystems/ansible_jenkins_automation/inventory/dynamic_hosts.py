#!/usr/bin/env python3
"""Portable dynamic inventory for a website fleet."""

import json


def main() -> None:
    inventory = {
        "mws": {
            "hosts": ["web-prod"],
            "vars": {
                "ansible_connection": "ssh",
                "ansible_user": "deploy",
            },
        },
        "_meta": {
            "hostvars": {
                "web-prod": {
                    "environment": "prod",
                    "role": "web",
                }
            }
        },
    }
    print(json.dumps(inventory))


if __name__ == "__main__":
    main()

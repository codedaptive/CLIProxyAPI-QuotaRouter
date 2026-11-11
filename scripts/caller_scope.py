"""Print the CPA caller scope for an existing client key without echoing the key."""

import getpass
import hashlib

if __name__ == "__main__":
    key = getpass.getpass("Desktop client key: ").strip()
    if not key:
        raise SystemExit("A client key is required")
    print(hashlib.sha256(b"cli-proxy-api:caller-scope:v1\x00" + key.encode()).hexdigest())

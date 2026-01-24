#!/usr/bin/env python3
"""
RLM test script with primary (Sonnet) and secondary (Haiku) models.

The Anthropic SDK automatically reads these env vars:
  - ANTHROPIC_API_KEY
  - ANTHROPIC_BASE_URL (for custom proxy)
"""
import os
import sys
from pathlib import Path

# Load .env file from rlm-test directory
from dotenv import load_dotenv
env_path = Path(__file__).parent / ".env"
if env_path.exists():
    load_dotenv(env_path)
    print(f"Loaded environment from {env_path}")

# Get API key
anthropic_key = os.environ.get("ANTHROPIC_API_KEY")
if not anthropic_key:
    print("ERROR: ANTHROPIC_API_KEY not found in environment")
    sys.exit(1)

# Model configuration
PRIMARY_MODEL = os.environ.get("RLM_PRIMARY_MODEL", "claude-sonnet-4-5-20250929")
SECONDARY_MODEL = os.environ.get("RLM_SECONDARY_MODEL", "claude-haiku-4-5-20251001")

base_url = os.environ.get("ANTHROPIC_BASE_URL")
print(f"Primary model: {PRIMARY_MODEL}")
print(f"Secondary model: {SECONDARY_MODEL}")
if base_url:
    print(f"Base URL: {base_url}")

from rlm import RLM

def main():
    print("\n=== RLM Test (Primary + Secondary) ===\n")

    # Primary model kwargs (Sonnet)
    primary_kwargs = {
        "model_name": PRIMARY_MODEL,
        "max_tokens": 4096,
        "api_key": anthropic_key,
    }

    # Secondary model kwargs (Haiku)
    secondary_kwargs = {
        "model_name": SECONDARY_MODEL,
        "max_tokens": 4096,
        "api_key": anthropic_key,
    }

    # Initialize RLM with primary and secondary
    rlm = RLM(
        backend="anthropic",
        backend_kwargs=primary_kwargs,
        other_backends=["anthropic"],
        other_backend_kwargs=[secondary_kwargs],
        environment="local",
        verbose=True
    )

    # Simple test prompt
    prompt = "Write a Python function that returns the sum of two numbers, then call it with 5 and 3."

    print(f"Prompt: {prompt}\n")
    print("Running RLM completion...\n")

    result = rlm.completion(prompt)

    print("=== Result ===")
    print(result.response)

    print("\n=== Test Complete ===")

if __name__ == "__main__":
    main()

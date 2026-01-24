#!/usr/bin/env python3
"""
Simple test script to verify RLM is working.
Supports OpenAI or Anthropic backends.

The Anthropic SDK automatically reads these env vars:
  - ANTHROPIC_API_KEY
  - ANTHROPIC_BASE_URL (for custom proxy)

The OpenAI SDK automatically reads:
  - OPENAI_API_KEY
  - OPENAI_BASE_URL (for custom proxy)
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

# Auto-detect which API key is available
openai_key = os.environ.get("OPENAI_API_KEY")
anthropic_key = os.environ.get("ANTHROPIC_API_KEY")

# Model override (optional)
model_override = os.environ.get("RLM_MODEL")

if anthropic_key:
    BACKEND = "anthropic"
    MODEL = model_override or "claude-sonnet-4-20250514"
    base_url = os.environ.get("ANTHROPIC_BASE_URL")
    print(f"Using Anthropic backend with {MODEL}")
    if base_url:
        print(f"  Base URL: {base_url}")
elif openai_key:
    BACKEND = "openai"
    MODEL = model_override or "gpt-4o-mini"
    base_url = os.environ.get("OPENAI_BASE_URL")
    print(f"Using OpenAI backend with {MODEL}")
    if base_url:
        print(f"  Base URL: {base_url}")
else:
    print("ERROR: No API key found")
    print()
    print("Set in rlm-test/.env:")
    print("  ANTHROPIC_API_KEY=your-key")
    print("  ANTHROPIC_BASE_URL=https://your-proxy  (optional)")
    print()
    print("Or:")
    print("  OPENAI_API_KEY=your-key")
    print("  OPENAI_BASE_URL=https://your-proxy  (optional)")
    sys.exit(1)

from rlm import RLM

def main():
    print("\n=== RLM Test ===\n")

    # Build backend kwargs - SDKs read base_url from env automatically
    backend_kwargs = {
        "model_name": MODEL,
        "max_tokens": 4096,  # Keep reasonable to avoid SDK timeout issues
    }

    # Initialize RLM
    rlm = RLM(
        backend=BACKEND,
        backend_kwargs=backend_kwargs,
        environment="local",
        verbose=True
    )

    # Simple test prompt that exercises the REPL loop
    prompt = "Write a Python function that returns the sum of two numbers, then call it with 5 and 3."

    print(f"Prompt: {prompt}\n")
    print("Running RLM completion...\n")

    result = rlm.completion(prompt)

    print("=== Result ===")
    print(result.response)

    print("\n=== Test Complete ===")

if __name__ == "__main__":
    main()

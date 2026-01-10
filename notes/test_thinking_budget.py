#!/usr/bin/env python3
"""
Test script to verify Antigravity thinking budget enforcement.

This script:
1. Sends requests with different thinking budget limits
2. Captures response usage (particularly reasoning_tokens/thinking tokens)
3. Compares actual usage against requested budget to verify enforcement
"""

import requests
import json
import time
import sys
from datetime import datetime

# Configuration
BASE_URL = "http://localhost:8317/v1"
API_KEY = "test"  # Your API key

# Antigravity model name for Claude with thinking
# NOT the Anthropic model name - this goes through Antigravity API
MODEL = "gemini-claude-opus-4-5-thinking"

# Test prompt that encourages thinking
TEST_PROMPT = """
Solve this step by step: What is 17 * 23 + 45 / 9 - 12 * 3?
Show your reasoning.
"""


def send_request(budget_tokens: int, max_tokens: int = 4096):
    """Send a request with a specific thinking budget."""

    payload = {
        "model": MODEL,
        "max_tokens": max_tokens,
        "thinking": {"type": "enabled", "budget_tokens": budget_tokens},
        "messages": [{"role": "user", "content": TEST_PROMPT}],
        "stream": False,  # Non-streaming for easier usage parsing
    }

    headers = {"Content-Type": "application/json", "Authorization": f"Bearer {API_KEY}"}

    print(f"\n{'='*60}")
    print(f"Testing with budget_tokens={budget_tokens}, max_tokens={max_tokens}")
    print(f"{'='*60}")

    start_time = time.time()

    try:
        response = requests.post(
            f"{BASE_URL}/chat/completions", headers=headers, json=payload, timeout=120
        )

        elapsed = time.time() - start_time

        if response.status_code != 200:
            print(f"ERROR: Status {response.status_code}")
            print(response.text[:500])
            return None

        data = response.json()

        # Extract usage info
        usage = data.get("usage", {})
        prompt_tokens = usage.get("prompt_tokens", "N/A")
        completion_tokens = usage.get("completion_tokens", "N/A")
        total_tokens = usage.get("total_tokens", "N/A")

        # reasoning_tokens is nested in completion_tokens_details for Antigravity
        completion_details = usage.get("completion_tokens_details", {})
        reasoning_tokens = completion_details.get("reasoning_tokens", "N/A")

        # Check for reasoning content in response
        choices = data.get("choices", [])
        has_reasoning = False
        reasoning_length = 0
        if choices:
            msg = choices[0].get("message", {})
            reasoning_content = msg.get("reasoning_content") or msg.get("reasoning")
            if reasoning_content:
                has_reasoning = True
                reasoning_length = len(reasoning_content)

        result = {
            "budget_requested": budget_tokens,
            "max_tokens": max_tokens,
            "reasoning_tokens": reasoning_tokens,
            "completion_tokens": completion_tokens,
            "prompt_tokens": prompt_tokens,
            "total_tokens": total_tokens,
            "elapsed_seconds": round(elapsed, 2),
            "has_reasoning_content": has_reasoning,
            "reasoning_content_chars": reasoning_length,
        }

        print(f"\nResults:")
        print(f"  Requested budget:    {budget_tokens} tokens")
        print(f"  Reasoning tokens:    {reasoning_tokens}")
        print(f"  Completion tokens:   {completion_tokens}")
        print(f"  Time elapsed:        {elapsed:.2f}s")
        print(f"  Has reasoning:       {has_reasoning}")

        if isinstance(reasoning_tokens, int) and reasoning_tokens > 0:
            if reasoning_tokens > budget_tokens:
                print(
                    f"\n  ⚠️  WARNING: reasoning_tokens ({reasoning_tokens}) > budget ({budget_tokens})"
                )
                print(f"      Budget may not be enforced or is approximate!")
            elif reasoning_tokens <= budget_tokens:
                print(f"\n  ✅ Budget appears to be respected")

        return result

    except requests.exceptions.Timeout:
        print(f"ERROR: Request timed out after 120s")
        return None
    except Exception as e:
        print(f"ERROR: {e}")
        return None


def main():
    print("=" * 60)
    print("ANTIGRAVITY THINKING BUDGET ENFORCEMENT TEST")
    print(f"Timestamp: {datetime.now().isoformat()}")
    print(f"Model: {MODEL}")
    print("=" * 60)

    # Test different budget values
    test_budgets = [
        # (budget_tokens, max_tokens)
        (1024, 8192),  # Small budget
        (4096, 8192),  # Medium budget
        (16384, 32768),  # Large budget
        (-1, 8192),  # Unlimited (default)
    ]

    results = []

    for budget, max_tok in test_budgets:
        result = send_request(budget, max_tok)
        if result:
            results.append(result)
        time.sleep(2)  # Rate limit buffer

    # Summary
    print("\n" + "=" * 60)
    print("SUMMARY")
    print("=" * 60)
    print(f"{'Budget':>10} | {'Reasoning Tokens':>18} | {'Time':>8} | {'Status'}")
    print("-" * 60)

    for r in results:
        budget = r["budget_requested"]
        reasoning = r["reasoning_tokens"]
        elapsed = r["elapsed_seconds"]

        if budget == -1:
            budget_str = "unlimited"
        else:
            budget_str = str(budget)

        if isinstance(reasoning, int):
            if budget == -1:
                status = "- (unlimited)"
            elif reasoning > budget:
                status = "⚠️ EXCEEDED"
            elif reasoning > budget * 0.8:
                status = "✅ Near limit"
            else:
                status = "✅ Under limit"
        else:
            status = "? Unknown"

        print(f"{budget_str:>10} | {str(reasoning):>18} | {elapsed:>6.1f}s | {status}")

    print("\n" + "=" * 60)
    print("ANALYSIS")
    print("=" * 60)

    # Check if smaller budgets correlate with fewer tokens
    numeric_results = [
        r
        for r in results
        if isinstance(r["reasoning_tokens"], int) and r["budget_requested"] != -1
    ]
    if len(numeric_results) >= 2:
        sorted_by_budget = sorted(numeric_results, key=lambda x: x["budget_requested"])

        print("\nCorrelation check (does lower budget = fewer tokens?):")
        for r in sorted_by_budget:
            print(
                f"  Budget {r['budget_requested']:>6} -> Reasoning tokens: {r['reasoning_tokens']}"
            )

        # Simple correlation check
        budgets = [r["budget_requested"] for r in sorted_by_budget]
        tokens = [r["reasoning_tokens"] for r in sorted_by_budget]

        if all(budgets[i] <= budgets[i + 1] for i in range(len(budgets) - 1)):
            if all(tokens[i] <= tokens[i + 1] for i in range(len(tokens) - 1)):
                print(
                    "\n✅ POSITIVE CORRELATION: Higher budget = more reasoning tokens"
                )
                print("   This suggests budget limits ARE being respected!")
            else:
                print("\n⚠️ NO CLEAR CORRELATION: Budget may not affect token usage")
                print("   Antigravity might be using server-side defaults")
    else:
        print("\nNot enough data for correlation analysis")


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""
Complete Thinking Block Test - 4 QUERIES

Tests: Can we omit OLDER thinking blocks and only include the LAST one?

Scenario:
1. User asks question → Assistant thinks + responds (Turn 1 thinking)
2. User asks to use tool → Assistant thinks + calls tool (Turn 2 thinking)
3. Send tool result with ONLY Turn 2's thinking (omit Turn 1's thinking)
4. If that works, test another tool loop with only the latest thinking
"""

import json
import os
import requests

# Load from environment variables (set in .env file)
NGROK_DOMAIN = os.environ.get("NGROK_DOMAIN", "localhost:8000")
PROXY_URL = f"https://{NGROK_DOMAIN}/v1/chat/completions"
API_KEY = os.environ.get("CLIPROXYAPI_KEY", "test-api-key")
MODEL = "claude-sonnet-4-20250514(xhigh)"


def log(msg):
    print(f">>> {msg}")


def send_request(messages, tools=None):
    payload = {"model": MODEL, "messages": messages, "max_tokens": 500, "stream": False}
    if tools:
        payload["tools"] = tools
    headers = {"Content-Type": "application/json", "Authorization": f"Bearer {API_KEY}"}
    return requests.post(PROXY_URL, headers=headers, json=payload, timeout=120).json()


def main():
    log(f"Model: {MODEL}")

    tools = [
        {
            "type": "function",
            "function": {
                "name": "get_time",
                "description": "Get time",
                "parameters": {"type": "object", "properties": {}},
            },
        }
    ]

    # QUERY 1: Simple question to get a thinking block (no tool)
    log("\n=== QUERY 1: Simple question (get thinking block) ===")
    messages = [{"role": "user", "content": "What is 2+2? Answer briefly."}]
    resp1 = send_request(messages)

    if "error" in resp1:
        log(f"ERROR: {resp1['error']}")
        return

    msg1 = resp1["choices"][0]["message"]
    thinking1 = msg1.get("reasoning_content") or msg1.get("reasoning")
    content1 = msg1.get("content", "4")
    log(f"Turn 1 has thinking: {bool(thinking1)}")
    log(f"Turn 1 content: {content1[:100]}...")

    # QUERY 2: Request that triggers tool call (another thinking block)
    log("\n=== QUERY 2: Tool request (get another thinking block) ===")
    messages = [
        {"role": "user", "content": "What is 2+2? Answer briefly."},
        (
            {"role": "assistant", "content": content1, "reasoning_content": thinking1}
            if thinking1
            else {"role": "assistant", "content": content1}
        ),
        {"role": "user", "content": "What time is it? Use the get_time tool."},
    ]
    resp2 = send_request(messages, tools)

    if "error" in resp2:
        log(f"ERROR: {resp2['error']}")
        return

    msg2 = resp2["choices"][0]["message"]
    thinking2 = msg2.get("reasoning_content") or msg2.get("reasoning")
    tool_calls = msg2.get("tool_calls", [])
    content2 = msg2.get("content")

    log(f"Turn 2 has thinking: {bool(thinking2)}")
    log(f"Turn 2 has tool_calls: {bool(tool_calls)}")

    if not tool_calls:
        log("No tool call. Cannot test tool loop scenario.")
        return

    tool_call_id = tool_calls[0]["id"]

    # QUERY 3: CRITICAL TEST - Send tool result with ONLY Turn 2's thinking
    # OMIT Turn 1's thinking block
    log("\n=== QUERY 3: Tool result - ONLY LAST thinking block (Turn 1 omitted) ===")

    messages_only_last_thinking = [
        {"role": "user", "content": "What is 2+2? Answer briefly."},
        # Turn 1: NO thinking_content here!
        {"role": "assistant", "content": content1},
        {"role": "user", "content": "What time is it? Use the get_time tool."},
        # Turn 2: Include thinking here (the LAST one before tool_use)
        (
            {
                "role": "assistant",
                "content": content2,
                "tool_calls": tool_calls,
                "reasoning_content": thinking2,
            }
            if thinking2
            else {"role": "assistant", "content": content2, "tool_calls": tool_calls}
        ),
        {
            "role": "tool",
            "tool_call_id": tool_call_id,
            "content": '{"time": "10:30 AM"}',
        },
    ]

    resp3 = send_request(messages_only_last_thinking, tools)

    log("\n=== RESULTS ===")

    if "error" in resp3:
        log(f"❌ ERROR: {resp3['error']}")
        if "thinking" in str(resp3).lower():
            log("CONCLUSION: Claude requires ALL thinking blocks from history!")
            log("=> Must cache ALL thinking blocks")
        else:
            log("Error may be unrelated to thinking blocks")
    else:
        content3 = resp3.get("choices", [{}])[0].get("message", {}).get("content", "")
        log(f"✅ SUCCESS! Response: {content3[:200]}...")
        log("CONCLUSION: Only the LAST thinking block is required!")
        log("=> Older thinking blocks can be safely evicted from cache")

    log("\nDone! (3 queries total)")


if __name__ == "__main__":
    main()

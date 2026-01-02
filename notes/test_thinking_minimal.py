#!/usr/bin/env python3
"""
Minimal Thinking Block Test - ONLY 2 QUERIES

Tests: Does Claude error if we don't send the thinking block from the
previous assistant turn during a tool loop?
"""

import json
import requests
import sys

PROXY_URL = "https://sensitive-cheryle-unwillfully.ngrok-free.dev/v1/chat/completions"
API_KEY = "muhsinun-api-key"
MODEL = "claude-sonnet-4-20250514(xhigh)"  # thinking-enabled


def log(msg):
    print(f">>> {msg}")


def send_request(messages, tools=None):
    payload = {
        "model": MODEL,
        "messages": messages,
        "max_tokens": 500,
        "stream": False,
    }
    if tools:
        payload["tools"] = tools

    headers = {
        "Content-Type": "application/json",
        "Authorization": f"Bearer {API_KEY}",
    }

    resp = requests.post(PROXY_URL, headers=headers, json=payload, timeout=120)
    return resp.json()


def main():
    log(f"Testing with model: {MODEL}")
    log(f"Proxy: {PROXY_URL}")

    tools = [
        {
            "type": "function",
            "function": {
                "name": "get_time",
                "description": "Get current time",
                "parameters": {"type": "object", "properties": {}},
            },
        }
    ]

    # QUERY 1: Initial request to trigger tool call with thinking
    log("\n=== QUERY 1: Request that triggers tool call ===")
    messages = [
        {
            "role": "system",
            "content": "Always use get_time tool when asked about time.",
        },
        {"role": "user", "content": "What time is it?"},
    ]

    resp1 = send_request(messages, tools)

    if "error" in resp1:
        log(f"ERROR in Query 1: {resp1['error']}")
        return

    # Extract response data
    choice = resp1.get("choices", [{}])[0]
    message = choice.get("message", {})
    tool_calls = message.get("tool_calls", [])
    content = message.get("content")
    reasoning = message.get("reasoning_content") or message.get("reasoning")

    log(f"Has tool_calls: {bool(tool_calls)}")
    log(f"Has reasoning: {bool(reasoning)}")

    if not tool_calls:
        log("No tool call returned. Cannot test tool loop scenario.")
        log("Response: " + json.dumps(resp1, indent=2)[:500])
        return

    tool_call_id = tool_calls[0].get("id")
    log(f"Tool call ID: {tool_call_id}")

    # QUERY 2: Send tool result WITHOUT the thinking block
    # This is the critical test - will Claude error?
    log("\n=== QUERY 2: Tool result WITHOUT thinking block ===")

    messages_without_thinking = [
        {
            "role": "system",
            "content": "Always use get_time tool when asked about time.",
        },
        {"role": "user", "content": "What time is it?"},
        {
            "role": "assistant",
            "content": content,  # Include content if any
            "tool_calls": tool_calls,  # Include tool calls
            # NO reasoning_content here!
        },
        {
            "role": "tool",
            "tool_call_id": tool_call_id,
            "content": '{"time": "10:30 AM"}',
        },
    ]

    resp2 = send_request(messages_without_thinking, tools)

    log("\n=== RESULTS ===")

    if "error" in resp2:
        log(f"❌ ERROR: {resp2['error']}")
        error_msg = str(resp2.get("error", {}))
        if "thinking" in error_msg.lower() or "signature" in error_msg.lower():
            log("CONCLUSION: Claude REQUIRES thinking blocks!")
            log("=> We MUST cache thinking blocks for tool loops")
        else:
            log(f"Error might be unrelated to thinking blocks")
    else:
        content2 = resp2.get("choices", [{}])[0].get("message", {}).get("content", "")
        log(f"✅ SUCCESS! Response: {content2[:200]}...")
        log("CONCLUSION: Claude does NOT require old thinking blocks!")
        log("=> Safe to evict cached thinking blocks")

    log("\nDone! (2 queries total)")


if __name__ == "__main__":
    main()

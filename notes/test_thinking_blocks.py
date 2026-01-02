#!/usr/bin/env python3
"""
Thinking Block Requirements Verification Test

This script tests whether Claude requires:
- Option A: Only the LAST thinking block during a tool loop
- Option B: ALL thinking blocks from the entire conversation history

The test performs multi-turn tool-use conversations with thinking enabled
and systematically removes thinking blocks from older turns to see what fails.
"""

import json
import os
import requests
import time
import argparse
from typing import Optional

# Configuration - Load from environment variables (set in .env file)
NGROK_DOMAIN = os.environ.get("NGROK_DOMAIN", "localhost:8000")
PROXY_URL = f"https://{NGROK_DOMAIN}/v1/chat/completions"
API_KEY = os.environ.get("CLIPROXYAPI_KEY", "test-api-key")

# Test models - one with thinking enabled
THINKING_MODEL = "claude-sonnet-4-20250514"  # Add (xhigh) suffix if needed for thinking


def log(msg: str):
    print(f"[{time.strftime('%H:%M:%S')}] {msg}")


def send_request(
    messages: list, tools: Optional[list] = None, stream: bool = False
) -> dict:
    """Send a chat completion request to the proxy."""
    payload = {
        "model": THINKING_MODEL,
        "messages": messages,
        "max_tokens": 1000,
        "stream": stream,
    }

    if tools:
        payload["tools"] = tools

    headers = {
        "Content-Type": "application/json",
        "Authorization": f"Bearer {API_KEY}",
    }

    log(f"Sending request with {len(messages)} messages...")

    if stream:
        response = requests.post(
            PROXY_URL, headers=headers, json=payload, stream=True, timeout=120
        )
        # Collect streaming response
        full_response = ""
        last_chunk = None
        for line in response.iter_lines():
            if line:
                line = line.decode("utf-8")
                if line.startswith("data: "):
                    data = line[6:]
                    if data.strip() == "[DONE]":
                        break
                    try:
                        chunk = json.loads(data)
                        last_chunk = chunk
                        if "choices" in chunk and chunk["choices"]:
                            delta = chunk["choices"][0].get("delta", {})
                            if "content" in delta:
                                full_response += delta["content"]
                    except json.JSONDecodeError:
                        pass
        return {"full_response": full_response, "last_chunk": last_chunk}
    else:
        response = requests.post(PROXY_URL, headers=headers, json=payload, timeout=120)
        return response.json()


def create_tool_definition():
    """Create a simple tool for testing."""
    return [
        {
            "type": "function",
            "function": {
                "name": "get_current_time",
                "description": "Get the current time",
                "parameters": {"type": "object", "properties": {}, "required": []},
            },
        }
    ]


def extract_thinking_and_tool_calls(response: dict):
    """Extract thinking content, signature, and tool calls from response."""
    result = {
        "thinking": None,
        "signature": None,
        "content": None,
        "tool_calls": None,
    }

    if "choices" in response and response["choices"]:
        message = response["choices"][0].get("message", {})
        result["content"] = message.get("content")
        result["tool_calls"] = message.get("tool_calls")

        # Check for reasoning_content (OpenAI format)
        result["thinking"] = message.get("reasoning_content") or message.get(
            "reasoning"
        )

    return result


def test_scenario_1_only_last_thinking_block():
    """
    Test: Does Claude work if we only provide the LAST thinking block?

    Scenario:
    1. User asks to use tool (Turn 1) -> Assistant thinks + calls tool
    2. User provides tool result (Turn 2) -> Assistant responds with thinking
    3. User asks a follow-up that needs tool (Turn 3) -> Assistant thinks + calls tool
    4. User provides tool result (Turn 4)
       - We include thinking block from Turn 3 only
       - We REMOVE thinking block from Turn 1

    Expected: If only last thinking block is needed, this should succeed.
    """
    log("\n" + "=" * 60)
    log("TEST SCENARIO 1: Only Last Thinking Block")
    log("=" * 60)

    tools = create_tool_definition()

    # Turn 1: Initial request that should trigger tool use
    messages = [
        {
            "role": "system",
            "content": "You are a helpful assistant. Always use the get_current_time tool when asked about time.",
        },
        {"role": "user", "content": "What time is it right now?"},
    ]

    log("Turn 1: Sending initial tool request...")
    response1 = send_request(messages, tools=tools)
    log(f"Response 1: {json.dumps(response1, indent=2)[:500]}...")

    turn1_data = extract_thinking_and_tool_calls(response1)
    log(f"Turn 1 - Has thinking: {turn1_data['thinking'] is not None}")
    log(f"Turn 1 - Has tool_calls: {turn1_data['tool_calls'] is not None}")

    if not turn1_data["tool_calls"]:
        log("ERROR: Turn 1 did not produce a tool call. Cannot continue test.")
        return False

    # Turn 2: Provide tool result
    assistant_message_turn1 = {
        "role": "assistant",
        "content": turn1_data["content"],
        "tool_calls": turn1_data["tool_calls"],
    }
    # Include thinking if available (this is what we're testing)
    if turn1_data["thinking"]:
        assistant_message_turn1["reasoning_content"] = turn1_data["thinking"]

    tool_result_message = {
        "role": "tool",
        "tool_call_id": turn1_data["tool_calls"][0]["id"],
        "content": json.dumps({"time": "10:30 AM"}),
    }

    messages.append(assistant_message_turn1)
    messages.append(tool_result_message)

    log("Turn 2: Sending tool result...")
    response2 = send_request(messages, tools=tools)
    log(f"Response 2: {json.dumps(response2, indent=2)[:500]}...")

    turn2_data = extract_thinking_and_tool_calls(response2)
    log(f"Turn 2 - Has thinking: {turn2_data['thinking'] is not None}")

    # Turn 3: Another request that triggers tool use
    assistant_message_turn2 = {
        "role": "assistant",
        "content": turn2_data["content"] or "The current time is 10:30 AM.",
    }
    if turn2_data["thinking"]:
        assistant_message_turn2["reasoning_content"] = turn2_data["thinking"]

    messages.append(assistant_message_turn2)
    messages.append({"role": "user", "content": "And what about now, a minute later?"})

    log("Turn 3: Sending second tool request...")
    response3 = send_request(messages, tools=tools)
    log(f"Response 3: {json.dumps(response3, indent=2)[:500]}...")

    turn3_data = extract_thinking_and_tool_calls(response3)
    log(f"Turn 3 - Has thinking: {turn3_data['thinking'] is not None}")
    log(f"Turn 3 - Has tool_calls: {turn3_data['tool_calls'] is not None}")

    if not turn3_data["tool_calls"]:
        log("Turn 3 did not produce a tool call. Testing without tool loop.")

    # NOW THE CRITICAL TEST:
    # Turn 4: Provide tool result but REMOVE thinking from Turn 1

    log("\n--- CRITICAL TEST: Removing thinking from Turn 1 ---")

    # Rebuild messages without Turn 1 thinking
    messages_without_old_thinking = [
        messages[0],  # system
        messages[1],  # user turn 1
        {  # assistant turn 1 - WITHOUT reasoning_content
            "role": "assistant",
            "content": turn1_data["content"],
            "tool_calls": turn1_data["tool_calls"],
        },
        messages[3],  # tool result turn 1
    ]

    if len(messages) > 4:
        messages_without_old_thinking.append(
            {  # assistant turn 2 - WITHOUT reasoning_content
                "role": "assistant",
                "content": turn2_data["content"] or "The current time is 10:30 AM.",
            }
        )
        messages_without_old_thinking.append(messages[5])  # user turn 3

        if turn3_data["tool_calls"]:
            # Include Turn 3's thinking (the LAST one)
            assistant_msg_turn3 = {
                "role": "assistant",
                "content": turn3_data["content"],
                "tool_calls": turn3_data["tool_calls"],
            }
            if turn3_data["thinking"]:
                assistant_msg_turn3["reasoning_content"] = turn3_data["thinking"]

            messages_without_old_thinking.append(assistant_msg_turn3)
            messages_without_old_thinking.append(
                {
                    "role": "tool",
                    "tool_call_id": turn3_data["tool_calls"][0]["id"],
                    "content": json.dumps({"time": "10:31 AM"}),
                }
            )

    log(
        f"Sending request with {len(messages_without_old_thinking)} messages (old thinking removed)..."
    )

    try:
        response4 = send_request(messages_without_old_thinking, tools=tools)
        log(f"Response 4: {json.dumps(response4, indent=2)[:500]}...")

        if "error" in response4:
            log(f"RESULT: ERROR - {response4['error']}")
            log("CONCLUSION: Claude REQUIRES thinking blocks from older turns")
            return False
        else:
            log("RESULT: SUCCESS - Request completed without error")
            log("CONCLUSION: Claude only needs the LAST thinking block!")
            return True
    except Exception as e:
        log(f"RESULT: EXCEPTION - {e}")
        return False


def test_scenario_2_no_thinking_blocks():
    """
    Test: What happens if we provide NO thinking blocks at all?

    This tests the baseline - does Claude error when thinking blocks are
    completely missing in a tool loop?
    """
    log("\n" + "=" * 60)
    log("TEST SCENARIO 2: No Thinking Blocks at All")
    log("=" * 60)

    tools = create_tool_definition()

    # Simulated multi-turn with tool use but NO thinking blocks
    messages = [
        {
            "role": "system",
            "content": "You are a helpful assistant. Always use the get_current_time tool when asked about time.",
        },
        {"role": "user", "content": "What time is it?"},
        {
            "role": "assistant",
            "content": None,
            "tool_calls": [
                {
                    "id": "call_test123",
                    "type": "function",
                    "function": {"name": "get_current_time", "arguments": "{}"},
                }
            ],
        },
        {
            "role": "tool",
            "tool_call_id": "call_test123",
            "content": json.dumps({"time": "10:30 AM"}),
        },
    ]

    log("Sending request with NO thinking blocks in history...")

    try:
        response = send_request(messages, tools=tools)
        log(f"Response: {json.dumps(response, indent=2)[:500]}...")

        if "error" in response:
            log(f"RESULT: ERROR - {response['error']}")
            log("CONCLUSION: Claude ERRORS when thinking blocks are missing")
            return False
        else:
            log("RESULT: SUCCESS - Request completed without error")
            log(
                "CONCLUSION: Claude does NOT require thinking blocks (or thinking is disabled)"
            )
            return True
    except Exception as e:
        log(f"RESULT: EXCEPTION - {e}")
        return False


def main():
    parser = argparse.ArgumentParser(
        description="Test Claude thinking block requirements"
    )
    parser.add_argument("--proxy-url", default=PROXY_URL, help="Proxy URL")
    parser.add_argument("--api-key", default=API_KEY, help="API key")
    parser.add_argument("--model", default=THINKING_MODEL, help="Model to test")
    args = parser.parse_args()

    global PROXY_URL, API_KEY, THINKING_MODEL
    PROXY_URL = args.proxy_url
    API_KEY = args.api_key
    THINKING_MODEL = args.model

    log("Starting Thinking Block Verification Tests")
    log(f"Proxy URL: {PROXY_URL}")
    log(f"Model: {THINKING_MODEL}")
    log("")

    # Run tests
    results = {}

    results["scenario_2_no_thinking"] = test_scenario_2_no_thinking_blocks()

    log("\n" + "=" * 60)
    log("Waiting 5 seconds before next test...")
    time.sleep(5)

    results["scenario_1_only_last"] = test_scenario_1_only_last_thinking_block()

    # Summary
    log("\n" + "=" * 60)
    log("TEST SUMMARY")
    log("=" * 60)
    for test, passed in results.items():
        status = "PASSED ✓" if passed else "FAILED ✗"
        log(f"  {test}: {status}")

    log("\n" + "=" * 60)
    log("FINAL CONCLUSIONS")
    log("=" * 60)

    if results.get("scenario_2_no_thinking"):
        log("- Thinking blocks may be disabled or not required for this model")
        log("- Run with a -thinking model to get accurate results")

    if results.get("scenario_1_only_last"):
        log("- CONFIRMED: Only the LAST thinking block is required")
        log(
            "- Cache strategy: Store only the most recent thinking block per conversation"
        )
        log("- Eviction safety: SAFE - older thinking blocks can be evicted")
    else:
        log("- WARNING: Could not confirm single-thinking-block requirement")
        log("- May need to cache ALL thinking blocks (more memory)")

    log("\nDone!")


if __name__ == "__main__":
    main()

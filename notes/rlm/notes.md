RLM paper: https://arxiv.org/pdf/2512.24601v1

we will use this repo: https://github.com/alexzhang13/rlm

## Overview

RLM (Reasoning Language Model) integration enables transparent 10M+ token context windows for any supported model. Users interact with the proxy as if using a standard model, but behind the scenes the RLM harness manages a REPL loop to handle extended context.

## Model Naming Convention

Prefix any model with `rlm-` to enable RLM mode.

### Single Model (same primary and secondary)
```
rlm-<model>
```
Example: `rlm-gpt-5` → primary: gpt-5, secondary: gpt-5

### Dual Model (different secondary)
```
rlm-<primary>--<secondary>
```
Example: `rlm-gpt-5--gpt-5-mini` → primary: gpt-5, secondary: gpt-5-mini

Note: Double dash (`--`) separates primary from secondary model.

## How It Works

1. User requests model `rlm-gpt-5`
2. Proxy parses the prefix and extracts model name(s)
3. Proxy invokes RLM harness with primary/secondary models
4. RLM harness manages the REPL loop internally
5. User receives responses as if using a normal model with massive context

## User Experience

- Transparent: User doesn't need to know about RLM internals
- Same API: Standard OpenAI-compatible requests work unchanged
- Extended context: Up to 10M tokens available
- Flexible: Can mix powerful primary with cheaper secondary model

## Implementation Notes

- TODO: Parse `rlm-` prefix in model routing
- TODO: Integrate rlmharness as subprocess/library
- TODO: Map model names to actual provider models
- TODO: Handle streaming responses from RLM loop
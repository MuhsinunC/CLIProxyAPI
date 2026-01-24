# RLM Test

## Setup

```bash
./setup.sh
```

Or manually:
```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
```

## Configure

Copy `.env.example` to `.env` and add your credentials.

## Run

```bash
source .venv/bin/activate
python test_rlm.py
```

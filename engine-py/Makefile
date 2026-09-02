.PHONY: install fixture test mock rpc lint clean

VENV ?= ../.venv
PY := $(VENV)/bin/python
export PYTHONPATH := $(CURDIR)

install:
	$(PY) -m pip install -e . && $(PY) -m pip install pytest ruff pyahocorasick

fixture:
	$(PY) tests/make_fixture.py

test: fixture
	$(PY) -m pytest tests/ -q

mock:
	$(PY) tests/mock_llm.py 8123

rpc: fixture
	PAPERKO_LLM_URL=http://127.0.0.1:8123/v1 $(PY) tests/rpc_smoke.py

lint:
	$(VENV)/bin/ruff check translate_engine || true

clean:
	rm -rf **/__pycache__ .pytest_cache tests/fixtures/*.pdf

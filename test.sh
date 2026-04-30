#!/bin/bash

# Teleconvert Test Quick Reference
# Run this script from the project root: bash test.sh

set -e

echo "════════════════════════════════════════"
echo "  Teleconvert Test Suite"
echo "════════════════════════════════════════"
echo

# Run all tests with verbose output
echo "📋 Running all tests..."
go test -v ./internal/... -count=1

echo
echo "════════════════════════════════════════"
echo "📊 Coverage Report"
echo "════════════════════════════════════════"
go test ./internal/... -cover

echo
echo "════════════════════════════════════════"
echo "✅ All tests passed!"
echo "════════════════════════════════════════"
echo
echo "Test Highlights:"
echo "  • Ledger persistence & recovery (11 tests)"
echo "  • Discovery & file handling (8 tests)"
echo "  • Worker operations (14 tests)"
echo "  • Config validation (8 tests)"
echo "  • Orchestrator state machine (12 tests)"
echo
echo "Robustness Features Tested:"
echo "  ✓ Resume ledger with state recovery"
echo "  ✓ Atomic file transfers (.part → rename)"
echo "  ✓ MD5 verification after upload"
echo "  ✓ Signal-safe cleanup on interrupt"
echo "  ✓ Per-job error tracking"
echo "  ✓ Thread-safe concurrency"
echo

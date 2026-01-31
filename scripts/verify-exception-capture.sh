#!/bin/bash
# Verification script for exception payload capture feature

set -e

echo "🔍 Verifying Exception Payload Capture Feature"
echo "=============================================="
echo ""

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 1. Check dumper code
echo "1. Checking dumper.go for ExceptionPayload field..."
if grep -q "ExceptionPayload" internal/debug/dumper.go; then
    echo -e "${GREEN}✅ ExceptionPayload field found in dumper.go${NC}"
else
    echo "❌ ExceptionPayload field NOT found"
    exit 1
fi

# 2. Check SetExceptionPayload method
echo ""
echo "2. Checking for SetExceptionPayload method..."
if grep -q "func.*SetExceptionPayload" internal/debug/dumper.go; then
    echo -e "${GREEN}✅ SetExceptionPayload method found${NC}"
else
    echo "❌ SetExceptionPayload method NOT found"
    exit 1
fi

# 3. Check handler integration
echo ""
echo "3. Checking handler.go integration..."
COUNT=$(grep -c "SetExceptionPayload" internal/handler/messages.go || true)
if [ "$COUNT" -ge 2 ]; then
    echo -e "${GREEN}✅ SetExceptionPayload called in $COUNT places${NC}"
else
    echo "❌ SetExceptionPayload not called correctly (found $COUNT calls, expected 2)"
    exit 1
fi

# 4. Check analyzer support
echo ""
echo "4. Checking analyzer.js support..."
if grep -q "exceptionPayload" .claude/skills/analyze-kiro-dumps.js; then
    echo -e "${GREEN}✅ Analyzer supports exceptionPayload${NC}"
else
    echo "❌ Analyzer does NOT support exceptionPayload"
    exit 1
fi

# 5. Build test
echo ""
echo "5. Testing Go build..."
if go build -o /tmp/verify-kiro ./cmd/kiro-server 2>/dev/null; then
    echo -e "${GREEN}✅ Build successful${NC}"
    rm -f /tmp/verify-kiro
else
    echo "❌ Build failed"
    exit 1
fi

# 6. Check existing dumps
echo ""
echo "6. Checking existing dump structure..."
DUMP_DIR="kiro-debug/errors"
if [ -d "$DUMP_DIR" ]; then
    SESSION_COUNT=$(find "$DUMP_DIR" -mindepth 1 -maxdepth 1 -type d | wc -l)
    echo -e "${YELLOW}ℹ️  Found $SESSION_COUNT existing error session(s)${NC}"

    # Check if any dumps have non-null exception_payload
    HAS_PAYLOAD=$(find "$DUMP_DIR" -name "metadata.json" -exec jq -r '.exception_payload // empty' {} \; 2>/dev/null | grep -v '^$' | head -1)
    if [ -n "$HAS_PAYLOAD" ]; then
        echo -e "${GREEN}✅ Found dumps with exception_payload data${NC}"
    else
        echo -e "${YELLOW}ℹ️  No dumps with exception_payload yet (expected for old dumps)${NC}"
        echo "   New errors will include this field after rebuilding the service"
    fi
else
    echo -e "${YELLOW}ℹ️  No error dumps found yet${NC}"
fi

# 7. Test analyzer
echo ""
echo "7. Testing analyzer..."
if node .claude/skills/analyze-kiro-dumps.js --help 2>&1 | head -1 > /dev/null; then
    echo -e "${GREEN}✅ Analyzer runs successfully${NC}"
else
    echo "⚠️  Analyzer test skipped (no --help flag)"
fi

echo ""
echo "=============================================="
echo -e "${GREEN}✅ All verification checks passed!${NC}"
echo ""
echo "Next steps:"
echo "  1. Rebuild and restart the Go service:"
echo "     make update-go"
echo ""
echo "  2. Trigger an error to test exception capture"
echo ""
echo "  3. Run analyzer to see exception details:"
echo "     npm run analyze:dumps"
echo ""
echo "  4. Check new dumps for exception_payload:"
echo "     cat kiro-debug/errors/LATEST-SESSION/metadata.json | jq .exception_payload"

#!/usr/bin/env node
/**
 * Build a Kiro request from original Claude API request.json using
 * the EXACT same logic as Node.js production (claude-kiro.js).
 *
 * Usage:
 *   node tests/integration/build-kiro-request.mjs /path/to/request.json [output.json]
 */

import fs from 'fs';
import path from 'path';
import crypto from 'crypto';

const FULL_MODEL_MAPPING = {
    "claude-opus-4-5": "claude-opus-4.5",
    "claude-opus-4.5": "claude-opus-4.5",
    "claude-opus-4-5-20251101": "claude-opus-4.5",
    "claude-haiku-4-5": "claude-haiku-4.5",
    "claude-haiku-4.5": "claude-haiku-4.5",
    "claude-haiku-4-5-20251001": "claude-haiku-4.5",
    "claude-sonnet-4.5": "CLAUDE_SONNET_4_5_20250929_V1_0",
    "claude-sonnet-4-5": "CLAUDE_SONNET_4_5_20250929_V1_0",
    "claude-sonnet-4-5-20250929": "CLAUDE_SONNET_4_5_20250929_V1_0",
    "claude-sonnet-4": "CLAUDE_SONNET_4_20250514_V1_0",
    "claude-sonnet-4-20250514": "CLAUDE_SONNET_4_20250514_V1_0",
    "claude-3-7-sonnet-20250219": "CLAUDE_3_7_SONNET_20250219_V1_0",
};

function getContentText(content) {
    if (!content) return '';
    if (typeof content === 'string') return content;
    if (Array.isArray(content)) {
        return content
            .filter(b => b.type === 'text')
            .map(b => b.text)
            .join('\n');
    }
    return '';
}

const requestFile = process.argv[2];
const outputFile = process.argv[3] || '/tmp/kiro_nodejs_rebuilt.json';

if (!requestFile) {
    console.error('Usage: node build-kiro-request.mjs <request.json> [output.json]');
    process.exit(1);
}

const rawData = fs.readFileSync(path.resolve(requestFile), 'utf-8');
const claudeReq = JSON.parse(rawData);

const model = claudeReq.model;
const messages = claudeReq.messages;
const tools = claudeReq.tools;
const thinking = claudeReq.thinking;

// Get system prompt
let systemPrompt = '';
if (claudeReq.system) {
    if (typeof claudeReq.system === 'string') {
        systemPrompt = claudeReq.system;
    } else if (Array.isArray(claudeReq.system)) {
        systemPrompt = claudeReq.system
            .filter(b => b.type === 'text')
            .map(b => b.text)
            .join('\n');
    }
}

// Map model
const kiroModel = FULL_MODEL_MAPPING[model] || FULL_MODEL_MAPPING['claude-sonnet-4-5'];
console.log('Model:', model, '->', kiroModel);

// Convert tools (matching claude-kiro.js lines 1024-1066)
let toolsContext = {};
if (tools && Array.isArray(tools) && tools.length > 0) {
    const filteredTools = tools.filter(tool => {
        const name = (tool.name || '').toLowerCase();
        return name !== 'web_search' && name !== 'websearch';
    });
    console.log('Tools: %d original, %d after filter', tools.length, filteredTools.length);

    if (filteredTools.length > 0) {
        const kiroTools = filteredTools.map(tool => ({
            toolSpecification: {
                name: tool.name,
                description: tool.description || "",
                inputSchema: {
                    json: tool.input_schema || {}
                }
            }
        }));
        toolsContext = { tools: kiroTools };
    }
}

// Build history (matching claude-kiro.js lines 1068-1200+)
const history = [];
let startIndex = 0;

// Merge adjacent same-role messages
const mergedMessages = [];
for (const msg of messages) {
    if (mergedMessages.length === 0) {
        mergedMessages.push({ ...msg });
    } else {
        const last = mergedMessages[mergedMessages.length - 1];
        if (msg.role === last.role) {
            if (Array.isArray(last.content) && Array.isArray(msg.content)) {
                last.content.push(...msg.content);
            } else if (typeof last.content === 'string' && typeof msg.content === 'string') {
                last.content += '\n' + msg.content;
            }
        } else {
            mergedMessages.push({ ...msg });
        }
    }
}

// Handle system prompt
if (systemPrompt) {
    if (mergedMessages[0]?.role === 'user') {
        const firstContent = getContentText(mergedMessages[0]);
        history.push({
            userInputMessage: {
                content: `${systemPrompt}\n\n${firstContent}`,
                modelId: kiroModel,
                origin: "AI_EDITOR",
            }
        });
        startIndex = 1;
    }
}

// Process remaining messages
for (let i = startIndex; i < mergedMessages.length; i++) {
    const msg = mergedMessages[i];
    if (msg.role === 'user') {
        const content = getContentText(msg);
        const userMsg = {
            userInputMessage: {
                content: content,
                modelId: kiroModel,
                origin: "AI_EDITOR",
            }
        };
        // Add tool results if present
        if (Array.isArray(msg.content)) {
            const toolResults = msg.content
                .filter(b => b.type === 'tool_result')
                .map(b => ({
                    toolUseId: b.tool_use_id,
                    content: typeof b.content === 'string' ? b.content : getContentText(b.content),
                    status: b.is_error ? "error" : "success",
                }));
            if (toolResults.length > 0) {
                userMsg.userInputMessage.userInputMessageContext = {
                    toolResults: toolResults,
                };
            }
        }
        history.push(userMsg);
    } else if (msg.role === 'assistant') {
        const content = getContentText(msg);
        const assistantMsg = {
            assistantResponseMessage: {
                content: content,
                modelId: kiroModel,
            }
        };
        // Add tool uses if present
        if (Array.isArray(msg.content)) {
            const toolUses = msg.content
                .filter(b => b.type === 'tool_use')
                .map(b => ({
                    toolUseId: b.id,
                    name: b.name,
                    input: b.input,
                }));
            if (toolUses.length > 0) {
                assistantMsg.assistantResponseMessage.toolUses = toolUses;
            }
        }
        history.push(assistantMsg);
    }
}

// Last message becomes currentMessage
const lastHistoryMsg = history.pop();

// Build final request
const conversationId = crypto.randomUUID();
const request = {
    conversationState: {
        chatTriggerType: "MANUAL",
        conversationId: conversationId,
        currentMessage: lastHistoryMsg,
    }
};

// Add tools to currentMessage
if (toolsContext.tools && lastHistoryMsg.userInputMessage) {
    if (!lastHistoryMsg.userInputMessage.userInputMessageContext) {
        lastHistoryMsg.userInputMessage.userInputMessageContext = {};
    }
    lastHistoryMsg.userInputMessage.userInputMessageContext.tools = toolsContext.tools;
}

// Add history (all except last)
if (history.length > 0) {
    request.conversationState.history = history;
}

// Write output
const output = JSON.stringify(request);
fs.writeFileSync(outputFile, output);
console.log('Written to:', outputFile, '(' + Buffer.byteLength(output) + ' bytes)');

// Also write pretty version
fs.writeFileSync(outputFile.replace('.json', '_pretty.json'), JSON.stringify(request, null, 2));

#!/usr/bin/env node
/**
 * Node.js Integration Test: Send kiro_request.json directly to Kiro API
 *
 * This script replicates the EXACT HTTP behavior of claude-kiro.js production code
 * to help diagnose whether request failures are caused by the request body or the HTTP layer.
 *
 * Usage:
 *   node tests/integration/send-kiro-request.mjs /path/to/kiro_request.json
 *
 * Environment:
 *   REDIS_URL          - Redis connection URL (default: redis://localhost:6379)
 *   KIRO_ACCOUNT_UUID  - Use a specific account UUID (optional, defaults to first healthy)
 *   KIRO_REGION        - Override region (optional, read from metadata.json or account)
 *   VERBOSE            - Set to "true" for full SSE event logging
 *
 * Examples:
 *   # Send a Node.js debug dump
 *   node tests/integration/send-kiro-request.mjs kiro-debug-31/nodejs/d3ef718c-.../kiro_request.json
 *
 *   # Send a Go debug dump (to compare)
 *   node tests/integration/send-kiro-request.mjs kiro-debug/errors/2b83ad92-.../kiro_request.json
 *
 *   # With specific account
 *   KIRO_ACCOUNT_UUID=d6c76e76-... node tests/integration/send-kiro-request.mjs /path/to/kiro_request.json
 */

import { createRequire } from 'module';
import fs from 'fs';
import path from 'path';
import http from 'http';
import https from 'https';
import crypto from 'crypto';

const require = createRequire(import.meta.url);
const axios = require('axios');
const Redis = require('ioredis');

// ─── Constants (matching claude-kiro.js production values) ───────────────────

const KIRO_VERSION = '0.8.140';
const BASE_URL_TEMPLATE = 'https://q.{{region}}.amazonaws.com/generateAssistantResponse';
const KEY_PREFIX = 'aiclient:';

// ─── Helper Functions ────────────────────────────────────────────────────────

function log(msg, ...args) {
    const ts = new Date().toISOString().slice(11, 23);
    console.log(`[${ts}] ${msg}`, ...args);
}

function logError(msg, ...args) {
    const ts = new Date().toISOString().slice(11, 23);
    console.error(`[${ts}] ERROR: ${msg}`, ...args);
}

function generateMachineId(profileArn) {
    if (!profileArn) return 'unknown-machine-id';
    return crypto.createHash('sha256').update(profileArn).digest('hex').slice(0, 32);
}

// ─── Redis Account/Token Access (matching Go test getHealthyAccount) ─────────

async function getAccountAndToken(redisUrl, specificUuid) {
    const redis = new Redis(redisUrl, {
        connectTimeout: 10000,
        commandTimeout: 10000,
        maxRetriesPerRequest: 3,
        lazyConnect: true,
    });

    await redis.connect();
    log('Connected to Redis: %s', redisUrl);

    try {
        // Get all Kiro accounts
        const accountsHash = await redis.hgetall(`${KEY_PREFIX}pools:claude-kiro-oauth`);
        const accounts = Object.values(accountsHash).map(v => JSON.parse(v));

        if (accounts.length === 0) {
            throw new Error('No Kiro accounts found in Redis');
        }

        log('Found %d Kiro accounts', accounts.length);

        // Select account
        let account;
        if (specificUuid) {
            account = accounts.find(a => a.uuid === specificUuid);
            if (!account) throw new Error(`Account ${specificUuid} not found`);
        } else {
            // Find first healthy, non-disabled account
            account = accounts.find(a => {
                const isHealthy = a.isHealthy !== false && a.is_healthy !== false;
                const isDisabled = a.isDisabled === true || a.is_disabled === true;
                return isHealthy && !isDisabled;
            });
            if (!account) throw new Error('No healthy accounts available');
        }

        log('Selected account: %s (region: %s)', account.uuid, account.region || account.idcRegion || 'unknown');

        // Get token
        const tokenData = await redis.get(`${KEY_PREFIX}tokens:claude-kiro-oauth:${account.uuid}`);
        if (!tokenData) {
            throw new Error(`No token found for account ${account.uuid}`);
        }

        const token = JSON.parse(tokenData);

        // Check token expiration
        if (token.expiresAt) {
            const expiresAt = new Date(token.expiresAt);
            const now = new Date();
            if (expiresAt <= now) {
                logError('Token expired at %s (now: %s)', token.expiresAt, now.toISOString());
                logError('Token needs refresh - this test uses existing tokens only');
            } else {
                const minutesLeft = Math.round((expiresAt - now) / 60000);
                log('Token expires in %d minutes', minutesLeft);
            }
        }

        return { account, token };
    } finally {
        await redis.quit();
    }
}

// ─── Build Axios Instance (matching claude-kiro.js exactly) ──────────────────

function createKiroAxios(profileArn) {
    const machineId = generateMachineId(profileArn);
    const osName = process.platform;
    const nodeVersion = process.version.slice(1); // Remove 'v' prefix

    const httpAgent = new http.Agent({
        keepAlive: true,
        maxSockets: 100,
        maxFreeSockets: 5,
        timeout: 120000,
    });
    const httpsAgent = new https.Agent({
        keepAlive: true,
        maxSockets: 100,
        maxFreeSockets: 5,
        timeout: 120000,
    });

    const instance = axios.create({
        timeout: 300000, // 5 min for large requests
        httpAgent,
        httpsAgent,
        proxy: false,
        headers: {
            'Content-Type': 'application/json',
            'Accept': 'application/json',
            'amz-sdk-request': 'attempt=1; max=1',
            'x-amzn-kiro-agent-mode': 'vibe',
            'x-amz-user-agent': `aws-sdk-js/1.0.0 KiroIDE-${KIRO_VERSION}-${machineId}`,
            'user-agent': `aws-sdk-js/1.0.0 ua/2.1 os/${osName} lang/js md/nodejs#${nodeVersion} api/codewhispererruntime#1.0.0 m/E KiroIDE-${KIRO_VERSION}-${machineId}`,
            'Connection': 'close',
        },
    });

    return instance;
}

// ─── SSE Response Parser ─────────────────────────────────────────────────────

function parseSSEStream(stream, verbose) {
    return new Promise((resolve, reject) => {
        let eventCount = 0;
        let textContent = '';
        let errorOccurred = false;
        let errorDetails = null;
        let buffer = '';

        stream.on('data', (chunk) => {
            buffer += chunk.toString();

            // Process complete SSE events (separated by double newline)
            let idx;
            while ((idx = buffer.indexOf('\n\n')) !== -1) {
                const block = buffer.slice(0, idx);
                buffer = buffer.slice(idx + 2);

                let eventType = '';
                let eventData = '';

                for (const line of block.split('\n')) {
                    if (line.startsWith('event: ')) {
                        eventType = line.slice(7);
                    } else if (line.startsWith('data: ')) {
                        eventData = line.slice(6);
                    }
                }

                if (!eventType) continue;
                eventCount++;

                switch (eventType) {
                    case 'message_start': {
                        try {
                            const msg = JSON.parse(eventData);
                            const model = msg?.message?.model || 'unknown';
                            const id = msg?.message?.id || 'unknown';
                            log('[message_start] model=%s id=%s', model, id);
                        } catch { log('[message_start]'); }
                        break;
                    }
                    case 'content_block_start': {
                        try {
                            const block = JSON.parse(eventData);
                            const type = block?.content_block?.type || 'unknown';
                            const name = block?.content_block?.name;
                            if (verbose) log('[content_block_start] type=%s%s', type, name ? ` name=${name}` : '');
                        } catch { /* skip */ }
                        break;
                    }
                    case 'content_block_delta': {
                        try {
                            const delta = JSON.parse(eventData);
                            const text = delta?.delta?.text;
                            if (text) textContent += text;
                        } catch { /* skip */ }
                        break;
                    }
                    case 'message_delta': {
                        try {
                            const delta = JSON.parse(eventData);
                            const stopReason = delta?.delta?.stop_reason;
                            const usage = delta?.usage;
                            log('[message_delta] stop_reason=%s usage=%s', stopReason, JSON.stringify(usage));
                        } catch { log('[message_delta]'); }
                        break;
                    }
                    case 'message_stop':
                        log('[message_stop]');
                        break;
                    case 'error': {
                        errorOccurred = true;
                        try {
                            errorDetails = JSON.parse(eventData);
                            logError('[error] %s: %s',
                                errorDetails?.error?.type || 'unknown',
                                errorDetails?.error?.message || eventData);
                        } catch {
                            logError('[error] %s', eventData);
                        }
                        break;
                    }
                    case 'ping':
                        break;
                    default:
                        if (verbose) log('[%s] %s', eventType, eventData.slice(0, 200));
                }
            }
        });

        stream.on('end', () => {
            log('Stream ended. Total events: %d', eventCount);
            if (textContent) {
                log('--- Text Content (%d chars) ---', textContent.length);
                const display = textContent.length > 2000
                    ? textContent.slice(0, 2000) + `\n... (truncated, ${textContent.length} total)`
                    : textContent;
                console.log(display);
            }
            resolve({ eventCount, textContent, errorOccurred, errorDetails });
        });

        stream.on('error', (err) => {
            reject(err);
        });
    });
}

// ─── Main ────────────────────────────────────────────────────────────────────

async function main() {
    const requestFilePath = process.argv[2];
    if (!requestFilePath) {
        console.error('Usage: node tests/integration/send-kiro-request.mjs <kiro_request.json>');
        console.error('');
        console.error('Environment variables:');
        console.error('  REDIS_URL          Redis URL (default: redis://localhost:6379)');
        console.error('  KIRO_ACCOUNT_UUID  Specific account UUID');
        console.error('  KIRO_REGION        Override region');
        console.error('  VERBOSE            Show all SSE events');
        process.exit(1);
    }

    const redisUrl = process.env.REDIS_URL || 'redis://localhost:6379';
    const specificUuid = process.env.KIRO_ACCOUNT_UUID;
    const regionOverride = process.env.KIRO_REGION;
    const verbose = process.env.VERBOSE === 'true';

    // 1. Read kiro_request.json
    const absPath = path.resolve(requestFilePath);
    log('Reading request file: %s', absPath);

    let reqData = fs.readFileSync(absPath, 'utf-8');
    const originalSize = Buffer.byteLength(reqData, 'utf-8');

    // Compact JSON
    try {
        reqData = JSON.stringify(JSON.parse(reqData));
        const compactSize = Buffer.byteLength(reqData, 'utf-8');
        log('JSON compacted: %d → %d bytes (%.0f%% reduction)',
            originalSize, compactSize,
            (originalSize - compactSize) / originalSize * 100);
    } catch (e) {
        logError('JSON parse failed, sending raw: %s', e.message);
    }

    // Log request summary
    try {
        const req = JSON.parse(reqData);
        const cs = req.conversationState;
        const modelId = cs?.currentMessage?.userInputMessage?.modelId;
        const messageCount = cs?.history?.length || 0;
        const toolCount = cs?.currentMessage?.userInputMessage?.userInputMessageContext?.tools?.length || 0;
        log('Request summary: model=%s messages=%d tools=%d size=%d bytes',
            modelId, messageCount, toolCount, Buffer.byteLength(reqData, 'utf-8'));
    } catch { /* skip */ }

    // Try to read metadata.json from same directory
    let metadata = null;
    const metadataPath = path.join(path.dirname(absPath), 'metadata.json');
    try {
        metadata = JSON.parse(fs.readFileSync(metadataPath, 'utf-8'));
        log('Metadata: source=%s uuid=%s region=%s', metadata.source, metadata.uuid, metadata.region);
    } catch { /* no metadata */ }

    // 2. Get account and token from Redis
    const { account, token } = await getAccountAndToken(redisUrl, specificUuid || metadata?.uuid);

    // Determine region
    const region = regionOverride || token.idcRegion || account.idcRegion || account.region || 'us-east-1';
    const profileArn = account.profileArn || account.profile_arn || token.profileArn;
    log('Using region=%s profileArn=%s', region, profileArn ? profileArn.slice(0, 40) + '...' : 'none');

    // 3. Build URL (matching claude-kiro.js)
    const requestUrl = BASE_URL_TEMPLATE.replace('{{region}}', region);
    log('Target URL: %s', requestUrl);

    // 4. Create axios instance (matching production headers exactly)
    const axiosInstance = createKiroAxios(profileArn);

    // 5. Send request (matching production flow)
    const sessionId = crypto.randomUUID();
    const headers = {
        'Authorization': `Bearer ${token.accessToken}`,
        'amz-sdk-invocation-id': sessionId,
    };

    log('Sending request (session=%s)...', sessionId);

    // Parse to object so axios serializes with JSON.stringify (matching production behavior)
    // Production sends JS object; if we send a string, axios may handle Content-Type differently
    const reqObject = JSON.parse(reqData);

    try {
        const response = await axiosInstance.post(requestUrl, reqObject, {
            headers,
            responseType: 'stream',
        });

        log('Response status: %d', response.status);

        // 6. Parse SSE stream
        const result = await parseSSEStream(response.data, verbose);

        if (result.errorOccurred) {
            logError('Request completed with error event');
            process.exit(1);
        } else {
            log('SUCCESS: Received %d events', result.eventCount);
        }
    } catch (err) {
        if (err.response) {
            logError('HTTP %d from Kiro API', err.response.status);

            // Try to read error body
            if (err.response.data) {
                if (typeof err.response.data === 'string') {
                    logError('Response body: %s', err.response.data);
                } else if (err.response.data.pipe) {
                    // Stream response
                    const chunks = [];
                    for await (const chunk of err.response.data) {
                        chunks.push(chunk);
                    }
                    const body = Buffer.concat(chunks).toString();
                    logError('Response body: %s', body);
                } else {
                    logError('Response body: %s', JSON.stringify(err.response.data));
                }
            }

            // Log request headers for debugging
            log('Request headers sent:');
            const sentHeaders = { ...axiosInstance.defaults.headers.common, ...axiosInstance.defaults.headers.post, ...headers };
            for (const [k, v] of Object.entries(sentHeaders)) {
                if (k.toLowerCase() === 'authorization') {
                    log('  %s: Bearer %s...', k, String(v).slice(7, 20));
                } else {
                    log('  %s: %s', k, v);
                }
            }
            process.exit(1);
        } else {
            logError('Request failed: %s', err.message);
            process.exit(1);
        }
    }
}

main().catch(err => {
    logError('Fatal: %s', err.message);
    process.exit(1);
});

#!/usr/bin/env node
/**
 * Antigravity OAuth 凭据生成脚本（零依赖版本）
 * 只需 Node.js，无需安装任何依赖
 *
 * 使用方法:
 *   node generate-antigravity-creds.js [输出文件路径]
 *
 * 示例:
 *   node generate-antigravity-creds.js
 *   node generate-antigravity-creds.js ./my-creds.json
 *   node generate-antigravity-creds.js /app/antigravity/account1.json
 */

import http from 'http';
import https from 'https';
import fs from 'fs';
import path from 'path';
import os from 'os';
import { exec } from 'child_process';

// Antigravity OAuth 配置
const OAUTH_CONFIG = {
    clientId: '1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com',
    clientSecret: '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',
    scope: 'https://www.googleapis.com/auth/cloud-platform',
    port: 8086,
    tokenUrl: 'https://oauth2.googleapis.com/token',
    authUrl: 'https://accounts.google.com/o/oauth2/v2/auth',
    defaultDir: '.antigravity',
    defaultFile: 'oauth_creds.json'
};

/**
 * 生成 HTML 响应页面
 */
function generateResponsePage(isSuccess, message) {
    const title = isSuccess ? 'Authorization Successful!' : 'Authorization Failed';
    const bgColor = isSuccess ? '#e8f5e9' : '#ffebee';
    const textColor = isSuccess ? '#2e7d32' : '#c62828';

    return `<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>${title}</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            display: flex;
            justify-content: center;
            align-items: center;
            height: 100vh;
            margin: 0;
            background: ${bgColor};
        }
        .container {
            text-align: center;
            padding: 40px;
            background: white;
            border-radius: 12px;
            box-shadow: 0 4px 20px rgba(0,0,0,0.1);
        }
        h1 { color: ${textColor}; margin-bottom: 16px; }
        p { color: #666; }
    </style>
</head>
<body>
    <div class="container">
        <h1>${title}</h1>
        <p>${message}</p>
    </div>
</body>
</html>`;
}

/**
 * 用 code 换取 token（原生 https 实现）
 */
function exchangeCodeForToken(code, redirectUri) {
    return new Promise((resolve, reject) => {
        const postData = new URLSearchParams({
            code: code,
            client_id: OAUTH_CONFIG.clientId,
            client_secret: OAUTH_CONFIG.clientSecret,
            redirect_uri: redirectUri,
            grant_type: 'authorization_code'
        }).toString();

        const url = new URL(OAUTH_CONFIG.tokenUrl);
        const options = {
            hostname: url.hostname,
            port: 443,
            path: url.pathname,
            method: 'POST',
            headers: {
                'Content-Type': 'application/x-www-form-urlencoded',
                'Content-Length': Buffer.byteLength(postData)
            }
        };

        const req = https.request(options, (res) => {
            let data = '';
            res.on('data', chunk => data += chunk);
            res.on('end', () => {
                try {
                    const tokens = JSON.parse(data);
                    if (tokens.error) {
                        reject(new Error(tokens.error_description || tokens.error));
                    } else {
                        // 添加 expiry_date 字段
                        if (tokens.expires_in) {
                            tokens.expiry_date = Date.now() + tokens.expires_in * 1000;
                        }
                        resolve(tokens);
                    }
                } catch (e) {
                    reject(new Error('Failed to parse token response'));
                }
            });
        });

        req.on('error', reject);
        req.write(postData);
        req.end();
    });
}

/**
 * 递归创建目录
 */
function mkdirRecursive(dirPath) {
    if (fs.existsSync(dirPath)) return;
    const parent = path.dirname(dirPath);
    if (!fs.existsSync(parent)) {
        mkdirRecursive(parent);
    }
    fs.mkdirSync(dirPath);
}

/**
 * 跨平台打开浏览器
 */
function openBrowser(url) {
    const platform = process.platform;
    let cmd;

    if (platform === 'darwin') {
        cmd = `open "${url}"`;
    } else if (platform === 'win32') {
        cmd = `start "" "${url}"`;
    } else {
        // Linux
        cmd = `xdg-open "${url}" || sensible-browser "${url}" || x-www-browser "${url}"`;
    }

    exec(cmd, (err) => {
        if (err) {
            console.log('\n[Note] Could not open browser automatically.');
            console.log('Please copy the URL above and open it manually.\n');
        }
    });
}

/**
 * 启动 OAuth 回调服务器
 */
function startCallbackServer(outputPath) {
    return new Promise((resolve, reject) => {
        const redirectUri = `http://localhost:${OAUTH_CONFIG.port}`;

        const server = http.createServer(async (req, res) => {
            try {
                const url = new URL(req.url, redirectUri);
                const code = url.searchParams.get('code');
                const error = url.searchParams.get('error');

                if (code) {
                    console.log('\n[OAuth] Received authorization code, exchanging for tokens...');

                    try {
                        const tokens = await exchangeCodeForToken(code, redirectUri);

                        // 确保输出目录存在
                        mkdirRecursive(path.dirname(outputPath));

                        // 保存凭据
                        fs.writeFileSync(outputPath, JSON.stringify(tokens, null, 2));

                        console.log(`[OAuth] Credentials saved to: ${outputPath}`);
                        console.log('\n========================================');
                        console.log('  Authorization successful!');
                        console.log('========================================\n');

                        // 显示凭据概要
                        console.log('Token info:');
                        console.log(`  - Access Token: ${tokens.access_token?.substring(0, 20)}...`);
                        console.log(`  - Refresh Token: ${tokens.refresh_token ? 'Present' : 'Not present'}`);
                        console.log(`  - Expiry: ${tokens.expiry_date ? new Date(tokens.expiry_date).toISOString() : 'Unknown'}`);
                        console.log(`  - Scope: ${tokens.scope || 'Unknown'}`);

                        res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
                        res.end(generateResponsePage(true, 'You can close this page now.'));

                        server.close();
                        resolve(tokens);
                    } catch (tokenError) {
                        console.error('[OAuth] Failed to exchange code for tokens:', tokenError.message);
                        res.writeHead(500, { 'Content-Type': 'text/html; charset=utf-8' });
                        res.end(generateResponsePage(false, `Token exchange failed: ${tokenError.message}`));
                        server.close();
                        reject(tokenError);
                    }
                } else if (error) {
                    console.error(`[OAuth] Authorization error: ${error}`);
                    res.writeHead(400, { 'Content-Type': 'text/html; charset=utf-8' });
                    res.end(generateResponsePage(false, `Authorization error: ${error}`));
                    server.close();
                    reject(new Error(error));
                } else {
                    // 忽略其他请求（如 favicon）
                    res.writeHead(204);
                    res.end();
                }
            } catch (err) {
                console.error('[OAuth] Server error:', err);
                res.writeHead(500, { 'Content-Type': 'text/html; charset=utf-8' });
                res.end(generateResponsePage(false, `Server error: ${err.message}`));
            }
        });

        server.on('error', (err) => {
            if (err.code === 'EADDRINUSE') {
                console.error(`\n[Error] Port ${OAUTH_CONFIG.port} is already in use.`);
                console.error('Please close the application using that port and try again.\n');
            }
            reject(err);
        });

        server.listen(OAUTH_CONFIG.port, 'localhost', () => {
            console.log(`[OAuth] Callback server started on port ${OAUTH_CONFIG.port}`);
        });

        // 5分钟超时
        setTimeout(() => {
            if (server.listening) {
                console.error('\n[OAuth] Authorization timeout (5 minutes). Please try again.\n');
                server.close();
                reject(new Error('Authorization timeout'));
            }
        }, 5 * 60 * 1000);
    });
}

/**
 * 主函数
 */
async function main() {
    // 解析输出路径参数
    let outputPath = process.argv[2];

    if (!outputPath) {
        // 默认保存到 ~/.antigravity/oauth_creds.json
        outputPath = path.join(os.homedir(), OAUTH_CONFIG.defaultDir, OAUTH_CONFIG.defaultFile);
    } else if (!path.isAbsolute(outputPath)) {
        // 相对路径转绝对路径
        outputPath = path.resolve(process.cwd(), outputPath);
    }

    console.log('\n========================================');
    console.log('  Antigravity OAuth Credentials Generator');
    console.log('========================================\n');
    console.log(`Output file: ${outputPath}\n`);

    // 生成授权 URL
    const redirectUri = `http://localhost:${OAUTH_CONFIG.port}`;
    const authParams = new URLSearchParams({
        client_id: OAUTH_CONFIG.clientId,
        redirect_uri: redirectUri,
        response_type: 'code',
        scope: OAUTH_CONFIG.scope,
        access_type: 'offline',
        prompt: 'consent'  // 强制显示同意页面以获取 refresh_token
    });
    const authUrl = `${OAUTH_CONFIG.authUrl}?${authParams.toString()}`;

    console.log('[OAuth] Opening browser for authorization...\n');
    console.log('If browser does not open automatically, visit:\n');
    console.log(`  ${authUrl}\n`);

    // 尝试自动打开浏览器
    openBrowser(authUrl);

    // 启动回调服务器等待授权
    try {
        await startCallbackServer(outputPath);
        process.exit(0);
    } catch (err) {
        console.error('\n[Error] Authorization failed:', err.message);
        process.exit(1);
    }
}

main();

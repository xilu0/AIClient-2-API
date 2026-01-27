import { existsSync, readFileSync } from 'fs';
import { promises as fs } from 'fs';
import path from 'path';
import multer from 'multer';

/**
 * Helper function to broadcast events to UI clients
 * @param {string} eventType - The type of event
 * @param {any} data - The data to broadcast
 */
export function broadcastEvent(eventType, data) {
    if (global.eventClients && global.eventClients.length > 0) {
        const payload = typeof data === 'string' ? data : JSON.stringify(data);
        global.eventClients.forEach(client => {
            client.write(`event: ${eventType}\n`);
            client.write(`data: ${payload}\n\n`);
        });
    }
}

/**
 * Server-Sent Events for real-time updates
 */
export async function handleEvents(req, res) {
    res.writeHead(200, {
        'Content-Type': 'text/event-stream',
        'Cache-Control': 'no-cache',
        'Connection': 'keep-alive',
        'Access-Control-Allow-Origin': '*'
    });

    res.write('\n');

    // Store the response object for broadcasting
    if (!global.eventClients) {
        global.eventClients = [];
    }
    global.eventClients.push(res);

    // Keep connection alive
    const keepAlive = setInterval(() => {
        res.write(':\n\n');
    }, 30000);

    req.on('close', () => {
        clearInterval(keepAlive);
        global.eventClients = global.eventClients.filter(r => r !== res);
    });

    return true;
}

/**
 * Initialize UI management features
 * 优化版本：添加日志采样和批量广播，减��CPU占用
 */
export function initializeUIManagement() {
    // Initialize log broadcasting for UI
    if (!global.eventClients) {
        global.eventClients = [];
    }
    if (!global.logBuffer) {
        global.logBuffer = [];
    }
    if (!global.logBroadcastPending) {
        global.logBroadcastPending = false;
    }
    if (!global.pendingLogs) {
        global.pendingLogs = [];
    }

    // 日志采样率（可通过环境变量配置）
    const BASE_LOG_SAMPLE_RATE = parseFloat(process.env.LOG_SAMPLE_RATE || '1.0');
    const BATCH_BROADCAST_DELAY = parseInt(process.env.LOG_BATCH_DELAY || '100'); // 100ms批量广播

    // P3 Fix: High concurrency adaptive log throttling
    // Track request rate to automatically reduce logging under load
    let logCountInWindow = 0;
    let windowStartTime = Date.now();
    const LOG_WINDOW_MS = 1000; // 1 second window
    const HIGH_LOAD_THRESHOLD = parseInt(process.env.LOG_HIGH_LOAD_THRESHOLD || '50'); // logs per second threshold
    const HIGH_LOAD_SAMPLE_RATE = parseFloat(process.env.LOG_HIGH_LOAD_SAMPLE_RATE || '0.1'); // 10% sampling under high load

    /**
     * P3 Fix: Get adaptive sample rate based on current load
     */
    function getAdaptiveSampleRate() {
        const now = Date.now();

        // Reset window if expired
        if (now - windowStartTime > LOG_WINDOW_MS) {
            logCountInWindow = 0;
            windowStartTime = now;
        }

        logCountInWindow++;

        // Under high load, reduce sample rate
        if (logCountInWindow > HIGH_LOAD_THRESHOLD) {
            return Math.min(BASE_LOG_SAMPLE_RATE, HIGH_LOAD_SAMPLE_RATE);
        }

        return BASE_LOG_SAMPLE_RATE;
    }

    /**
     * 批量广播日志，减少广播频率
     */
    function scheduleBatchBroadcast() {
        if (global.logBroadcastPending) {
            return;
        }

        global.logBroadcastPending = true;
        setTimeout(() => {
            if (global.pendingLogs.length > 0) {
                // 批量广播最近的日志
                const logsToSend = global.pendingLogs.slice(-10);
                broadcastEvent('log_batch', logsToSend);
                global.pendingLogs = [];
            }
            global.logBroadcastPending = false;
        }, BATCH_BROADCAST_DELAY);
    }

    // Override console.log to broadcast logs
    const originalLog = console.log;
    console.log = function(...args) {
        originalLog.apply(console, args);

        // P3 Fix: Use adaptive sample rate based on current load
        const sampleRate = getAdaptiveSampleRate();
        if (Math.random() > sampleRate) {
            return;
        }

        // 使用 setImmediate 异步处理日志，避免阻塞主线程
        setImmediate(() => {
            const message = args.map(arg => {
                if (typeof arg === 'string') return arg;
                try {
                    return JSON.stringify(arg);
                } catch (e) {
                    if (arg instanceof Error) {
                        return `[Error: ${arg.message}]`;
                    }
                    return '[Complex Object]';
                }
            }).join(' ');

            const logEntry = {
                timestamp: new Date().toISOString(),
                level: 'info',
                message: message
            };

            global.logBuffer.push(logEntry);
            if (global.logBuffer.length > 100) {
                global.logBuffer.shift();
            }

            // 添加到待广播队列
            global.pendingLogs.push(logEntry);

            // 调度批量广播
            scheduleBatchBroadcast();
        });
    };

    // Override console.error to broadcast errors
    const originalError = console.error;
    console.error = function(...args) {
        originalError.apply(console, args);

        // 错误日志始终记录，不采样
        setImmediate(() => {
            const message = args.map(arg => {
                if (typeof arg === 'string') return arg;
                try {
                    return JSON.stringify(arg);
                } catch (e) {
                    if (arg instanceof Error) {
                        return `[Error: ${arg.message}] ${arg.stack || ''}`;
                    }
                    return '[Complex Object]';
                }
            }).join(' ');

            const logEntry = {
                timestamp: new Date().toISOString(),
                level: 'error',
                message: message
            };

            global.logBuffer.push(logEntry);
            if (global.logBuffer.length > 100) {
                global.logBuffer.shift();
            }

            // 错误日志立即广播
            broadcastEvent('log', logEntry);
        });
    };
}

// 配置multer中间件
const storage = multer.diskStorage({
    destination: async (req, file, cb) => {
        try {
            // multer在destination回调时req.body还未解析，先使用默认路径
            // 实际的provider会在文件上传完成后从req.body中获取
            const uploadPath = path.join(process.cwd(), 'configs', 'temp');
            await fs.mkdir(uploadPath, { recursive: true });
            cb(null, uploadPath);
        } catch (error) {
            cb(error);
        }
    },
    filename: (req, file, cb) => {
        const timestamp = Date.now();
        const sanitizedName = file.originalname.replace(/[^a-zA-Z0-9.-]/g, '_');
        cb(null, `${timestamp}_${sanitizedName}`);
    }
});

const fileFilter = (req, file, cb) => {
    const allowedTypes = ['.json', '.txt', '.key', '.pem', '.p12', '.pfx'];
    const ext = path.extname(file.originalname).toLowerCase();
    if (allowedTypes.includes(ext)) {
        cb(null, true);
    } else {
        cb(new Error('Unsupported file type'), false);
    }
};

export const upload = multer({
    storage,
    fileFilter,
    limits: {
        fileSize: 5 * 1024 * 1024 // 5MB限制
    }
});

/**
 * 处理 OAuth 凭据文件上传
 * @param {http.IncomingMessage} req - HTTP 请求对象
 * @param {http.ServerResponse} res - HTTP 响应对象
 * @param {Object} options - 可选配置
 * @param {Object} options.providerMap - 提供商类型映射表
 * @param {string} options.logPrefix - 日志前缀
 * @param {string} options.userInfo - 用户信息（用于日志）
 * @param {Object} options.customUpload - 自定义 multer 实例
 * @returns {Promise<boolean>} 始终返回 true 表示请求已处理
 */
export function handleUploadOAuthCredentials(req, res, options = {}) {
    const {
        providerMap = {},
        logPrefix = '[UI API]',
        userInfo = '',
        customUpload = null
    } = options;
    
    const uploadMiddleware = customUpload ? customUpload.single('file') : upload.single('file');
    
    return new Promise((resolve) => {
        uploadMiddleware(req, res, async (err) => {
            if (err) {
                console.error(`${logPrefix} File upload error:`, err.message);
                res.writeHead(400, { 'Content-Type': 'application/json' });
                res.end(JSON.stringify({
                    error: {
                        message: err.message || 'File upload failed'
                    }
                }));
                resolve(true);
                return;
            }

            try {
                if (!req.file) {
                    res.writeHead(400, { 'Content-Type': 'application/json' });
                    res.end(JSON.stringify({
                        error: {
                            message: 'No file was uploaded'
                        }
                    }));
                    resolve(true);
                    return;
                }

                // multer执行完成后，表单字段已解析到req.body中
                const providerType = req.body.provider || 'common';
                // 应用提供商映射（如果有）
                const provider = providerMap[providerType] || providerType;
                const tempFilePath = req.file.path;
                
                // 根据实际的provider移动文件到正确的目录
                let targetDir = path.join(process.cwd(), 'configs', provider);
                
                // 如果是kiro类型的凭证，需要再包裹一层文件夹
                if (provider === 'kiro') {
                    // 使用时间戳作为子文件夹名称，确保每个上传的文件都有独立的目录
                    const timestamp = Date.now();
                    const originalNameWithoutExt = path.parse(req.file.originalname).name;
                    const subFolder = `${timestamp}_${originalNameWithoutExt}`;
                    targetDir = path.join(targetDir, subFolder);
                }
                
                await fs.mkdir(targetDir, { recursive: true });
                
                const targetFilePath = path.join(targetDir, req.file.filename);
                await fs.rename(tempFilePath, targetFilePath);
                
                const relativePath = path.relative(process.cwd(), targetFilePath);

                // 广播更新事件
                broadcastEvent('config_update', {
                    action: 'add',
                    filePath: relativePath,
                    provider: provider,
                    timestamp: new Date().toISOString()
                });

                const userInfoStr = userInfo ? `, ${userInfo}` : '';
                console.log(`${logPrefix} OAuth credentials file uploaded: ${targetFilePath} (provider: ${provider}${userInfoStr})`);

                res.writeHead(200, { 'Content-Type': 'application/json' });
                res.end(JSON.stringify({
                    success: true,
                    message: 'File uploaded successfully',
                    filePath: relativePath,
                    originalName: req.file.originalname,
                    provider: provider
                }));
                resolve(true);

            } catch (error) {
                console.error(`${logPrefix} File upload processing error:`, error);
                res.writeHead(500, { 'Content-Type': 'application/json' });
                res.end(JSON.stringify({
                    error: {
                        message: 'File upload processing failed: ' + error.message
                    }
                }));
                resolve(true);
            }
        });
    });
}
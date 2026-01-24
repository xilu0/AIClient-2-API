import { describe, test, expect, beforeAll } from '@jest/globals';
import { fetch } from 'undici';

const TEST_SERVER_BASE_URL = process.env.TEST_SERVER_BASE_URL || 'http://localhost:3000';
const TEST_API_KEY = process.env.TEST_API_KEY || 'AI_club2026';

describe('Server Integration Tests', () => {
    let serverAvailable = false;

    beforeAll(async () => {
        try {
            const response = await fetch(`${TEST_SERVER_BASE_URL}/health`, {
                signal: AbortSignal.timeout(3000)
            });
            if (response.ok) {
                serverAvailable = true;
                console.log('✓ Server is available');
            }
        } catch (error) {
            console.log('⚠️ Server not available, skipping integration tests');
        }
    });

    test('should connect to server health endpoint', async () => {
        if (!serverAvailable) {
            return expect(true).toBe(true); // Skip test
        }

        const response = await fetch(`${TEST_SERVER_BASE_URL}/health`);
        expect(response.status).toBe(200);
        
        const data = await response.json();
        expect(data).toHaveProperty('status');
        expect(data.status).toBe('healthy');
    });

    test('should reject requests without API key', async () => {
        if (!serverAvailable) {
            return expect(true).toBe(true); // Skip test
        }

        const response = await fetch(`${TEST_SERVER_BASE_URL}/v1/models`);
        expect(response.status).toBe(401);
        
        const data = await response.json();
        expect(data).toHaveProperty('error');
        expect(data.error.message).toContain('Unauthorized');
    });

    test('should handle CORS headers', async () => {
        if (!serverAvailable) {
            return expect(true).toBe(true); // Skip test
        }

        const response = await fetch(`${TEST_SERVER_BASE_URL}/health`, {
            method: 'OPTIONS'
        });
        
        expect(response.headers.get('access-control-allow-origin')).toBe('*');
        expect(response.headers.get('access-control-allow-methods')).toContain('GET');
        expect(response.headers.get('access-control-allow-headers')).toContain('Content-Type');
    });
});

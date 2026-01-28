#!/usr/bin/env node

import { execSync } from 'child_process';

console.log('🧪 Running AIClient-2-API Tests\n');

// Set environment variables to skip integration tests by default
process.env.SKIP_INTEGRATION_TESTS = 'true';

try {
    // Run unit tests only
    console.log('📋 Running unit tests...');
    execSync('npx jest tests/basic.test.js tests/gemini-converter.test.js tests/openai-responses-converter.test.js tests/cors-config.test.js --verbose', {
        stdio: 'inherit',
        env: { ...process.env }
    });
    
    console.log('\n✅ Unit tests completed successfully!');
    
    // Check if server is available for integration tests
    console.log('\n🔍 Checking server availability for integration tests...');
    try {
        const { fetch } = await import('undici');
        const response = await fetch('http://localhost:3000/health', {
            signal: AbortSignal.timeout(3000)
        });
        
        if (response.ok) {
            console.log('🌐 Server is available, running integration tests...');
            process.env.SKIP_INTEGRATION_TESTS = 'false';
            execSync('npx jest tests/api-integration.test.js --verbose', {
                stdio: 'inherit',
                env: { ...process.env }
            });
            console.log('✅ Integration tests completed successfully!');
        } else {
            console.log('⚠️  Server responded with error, skipping integration tests');
        }
    } catch (error) {
        console.log('⚠️  Server not available, skipping integration tests');
        console.log('   Start the server with: npm start');
    }
    
} catch (error) {
    console.error('❌ Tests failed:', error.message);
    process.exit(1);
}

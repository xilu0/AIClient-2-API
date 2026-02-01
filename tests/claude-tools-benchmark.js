#!/usr/bin/env node
/**
 * Claude Code Tools Performance Benchmark
 *
 * Benchmarks the performance of Claude Code CLI tools:
 * 1. Read - File reading operations
 * 2. Write - File writing operations
 * 3. Glob - File pattern matching
 * 4. Grep - Content searching
 * 5. Bash - Command execution
 *
 * Usage: node tests/claude-tools-benchmark.js [options]
 *
 * Options:
 *   --iterations N    Number of iterations per test (default: 100)
 *   --tool <name>     Run only specific tool benchmark
 *   --verbose         Show detailed output
 */

import { readFile, writeFile, unlink, mkdir, rm } from 'fs/promises';
import { existsSync } from 'fs';
import { join } from 'path';
import { exec } from 'child_process';
import { promisify } from 'util';
import { fileURLToPath } from 'url';
import { dirname } from 'path';

const execAsync = promisify(exec);
const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

// Configuration
const DEFAULT_ITERATIONS = 100;
const TEST_DIR = '/tmp/claude-tools-benchmark';

// Performance thresholds (p95 latency in ms)
const THRESHOLDS = {
    'Read (small file)': 50,
    'Read (medium file)': 100,
    'Read (large file)': 150,
    'Write (small file)': 50,
    'Write (medium file)': 100,
    'Write (overwrite)': 50,
    'Glob (test files)': 100,
    'Glob (js files)': 150,
    'Glob (all files)': 200,
    'Grep (keyword)': 200,
    'Grep (regex)': 250,
    'Grep (with context)': 300,
    'Bash (echo)': 100,
    'Bash (ls)': 150,
    'Bash (git status)': 300
};

// Test files
const TEST_FILES = {
    small: join(__dirname, '..', 'package.json'),
    medium: join(__dirname, '..', 'test-claude-tools.js'),
    large: join(__dirname, '..', 'CLAUDE.md')
};

/**
 * Run benchmark for a single operation
 */
async function benchmark(name, iterations, operation) {
    const times = [];

    // Warmup run
    try {
        await operation(0);
    } catch (error) {
        // Ignore warmup errors
    }

    for (let i = 0; i < iterations; i++) {
        const start = process.hrtime.bigint();
        try {
            await operation(i);
        } catch (error) {
            // Record failed operations as max time
            times.push(1000);
            continue;
        }
        const end = process.hrtime.bigint();
        times.push(Number(end - start) / 1e6); // Convert to ms
    }

    times.sort((a, b) => a - b);

    const avg = times.reduce((a, b) => a + b, 0) / times.length;
    const min = times[0];
    const max = times[times.length - 1];
    const p50 = times[Math.floor(times.length * 0.5)];
    const p95 = times[Math.floor(times.length * 0.95)];
    const p99 = times[Math.floor(times.length * 0.99)];

    const threshold = THRESHOLDS[name] || 100;
    const passed = p95 < threshold;

    return {
        name,
        iterations,
        avg: avg.toFixed(2),
        min: min.toFixed(2),
        max: max.toFixed(2),
        p50: p50.toFixed(2),
        p95: p95.toFixed(2),
        p99: p99.toFixed(2),
        threshold,
        passed
    };
}

/**
 * Setup test environment
 */
async function setupTestEnvironment() {
    if (existsSync(TEST_DIR)) {
        await rm(TEST_DIR, { recursive: true, force: true });
    }
    await mkdir(TEST_DIR, { recursive: true });

    // Create test files
    const smallContent = 'x'.repeat(100);
    const mediumContent = 'x'.repeat(10000);
    const largeContent = 'x'.repeat(100000);

    await writeFile(join(TEST_DIR, 'small.txt'), smallContent);
    await writeFile(join(TEST_DIR, 'medium.txt'), mediumContent);
    await writeFile(join(TEST_DIR, 'large.txt'), largeContent);

    // Create nested directories for glob tests
    await mkdir(join(TEST_DIR, 'src'), { recursive: true });
    await mkdir(join(TEST_DIR, 'tests'), { recursive: true });
    await writeFile(join(TEST_DIR, 'src', 'index.js'), 'console.log("test");');
    await writeFile(join(TEST_DIR, 'tests', 'test.js'), 'test();');
    await writeFile(join(TEST_DIR, 'README.md'), '# Test');

    // Create file with searchable content
    const searchContent = `
MODEL_PROVIDER is a constant
function testFunction() {
    return MODEL_PROVIDER;
}
class TestClass {
    constructor() {
        this.provider = MODEL_PROVIDER;
    }
}
`.repeat(10);
    await writeFile(join(TEST_DIR, 'search.js'), searchContent);
}

/**
 * Cleanup test environment
 */
async function cleanupTestEnvironment() {
    if (existsSync(TEST_DIR)) {
        await rm(TEST_DIR, { recursive: true, force: true });
    }
}

/**
 * Read tool benchmarks
 */
async function benchmarkRead(iterations, verbose) {
    const results = [];

    if (verbose) console.log('\n[Read] Running benchmarks...');

    // Small file
    results.push(await benchmark('Read (small file)', iterations, async () => {
        await readFile(TEST_FILES.small, 'utf8');
    }));

    // Medium file
    results.push(await benchmark('Read (medium file)', iterations, async () => {
        await readFile(TEST_FILES.medium, 'utf8');
    }));

    // Large file
    results.push(await benchmark('Read (large file)', iterations, async () => {
        await readFile(TEST_FILES.large, 'utf8');
    }));

    return results;
}

/**
 * Write tool benchmarks
 */
async function benchmarkWrite(iterations, verbose) {
    const results = [];

    if (verbose) console.log('\n[Write] Running benchmarks...');

    // Small file
    const smallContent = 'x'.repeat(100);
    results.push(await benchmark('Write (small file)', iterations, async (i) => {
        await writeFile(join(TEST_DIR, `write-small-${i % 10}.txt`), smallContent);
    }));

    // Medium file
    const mediumContent = 'x'.repeat(10000);
    results.push(await benchmark('Write (medium file)', iterations, async (i) => {
        await writeFile(join(TEST_DIR, `write-medium-${i % 10}.txt`), mediumContent);
    }));

    // Overwrite existing file
    const overwritePath = join(TEST_DIR, 'overwrite.txt');
    await writeFile(overwritePath, 'initial content');
    results.push(await benchmark('Write (overwrite)', iterations, async (i) => {
        await writeFile(overwritePath, `content ${i}`);
    }));

    return results;
}

/**
 * Glob tool benchmarks (simulated with fs operations)
 */
async function benchmarkGlob(iterations, verbose) {
    const results = [];

    if (verbose) console.log('\n[Glob] Running benchmarks...');

    // Find test files
    results.push(await benchmark('Glob (test files)', iterations, async () => {
        await execAsync(`find ${TEST_DIR} -name "*.js" -type f`);
    }));

    // Find all JS files in project
    results.push(await benchmark('Glob (js files)', iterations, async () => {
        await execAsync(`find ${join(__dirname, '..')} -name "*.js" -type f | head -100`);
    }));

    // Find all files in test directory
    results.push(await benchmark('Glob (all files)', iterations, async () => {
        await execAsync(`find ${TEST_DIR} -type f`);
    }));

    return results;
}

/**
 * Grep tool benchmarks (simulated with grep command)
 */
async function benchmarkGrep(iterations, verbose) {
    const results = [];

    if (verbose) console.log('\n[Grep] Running benchmarks...');

    const searchFile = join(TEST_DIR, 'search.js');

    // Keyword search
    results.push(await benchmark('Grep (keyword)', iterations, async () => {
        await execAsync(`grep -n "MODEL_PROVIDER" ${searchFile}`);
    }));

    // Regex search
    results.push(await benchmark('Grep (regex)', iterations, async () => {
        await execAsync(`grep -nE "function\\s+\\w+" ${searchFile}`);
    }));

    // Search with context
    results.push(await benchmark('Grep (with context)', iterations, async () => {
        await execAsync(`grep -n -C 2 "MODEL_PROVIDER" ${searchFile}`);
    }));

    return results;
}

/**
 * Bash tool benchmarks
 */
async function benchmarkBash(iterations, verbose) {
    const results = [];

    if (verbose) console.log('\n[Bash] Running benchmarks...');

    // Simple echo command
    results.push(await benchmark('Bash (echo)', iterations, async () => {
        await execAsync('echo "test"');
    }));

    // List files
    results.push(await benchmark('Bash (ls)', iterations, async () => {
        await execAsync(`ls -la ${TEST_DIR}`);
    }));

    // Git status (if in git repo)
    if (existsSync(join(__dirname, '..', '.git'))) {
        results.push(await benchmark('Bash (git status)', iterations, async () => {
            await execAsync('git status --short', { cwd: join(__dirname, '..') });
        }));
    }

    return results;
}

/**
 * Print results table
 */
function printResults(results) {
    console.log('\n' + '='.repeat(110));
    console.log('Claude Code Tools Performance Benchmark Results');
    console.log('='.repeat(110));
    console.log(
        'Operation'.padEnd(25) +
        'Avg(ms)'.padStart(10) +
        'Min(ms)'.padStart(10) +
        'Max(ms)'.padStart(10) +
        'P50(ms)'.padStart(10) +
        'P95(ms)'.padStart(10) +
        'P99(ms)'.padStart(10) +
        'Target'.padStart(10) +
        'Status'.padStart(10)
    );
    console.log('─'.repeat(110));

    let allPassed = true;
    for (const r of results) {
        const status = r.passed ? '✅ PASS' : '❌ FAIL';
        if (!r.passed) allPassed = false;
        console.log(
            r.name.padEnd(25) +
            r.avg.padStart(10) +
            r.min.padStart(10) +
            r.max.padStart(10) +
            r.p50.padStart(10) +
            r.p95.padStart(10) +
            r.p99.padStart(10) +
            `<${r.threshold}`.padStart(10) +
            status.padStart(10)
        );
    }
    console.log('─'.repeat(110));

    return allPassed;
}

/**
 * Main benchmark runner
 */
async function runBenchmarks(options) {
    const { iterations, tool, verbose } = options;

    console.log(`\n🚀 Claude Code Tools Performance Benchmark`);
    console.log(`Iterations: ${iterations} per operation`);
    console.log(`Target: p95 within specified thresholds\n`);

    try {
        // Setup
        if (verbose) console.log('[Setup] Creating test environment...');
        await setupTestEnvironment();

        const allResults = [];

        // Run benchmarks
        if (!tool || tool === 'read') {
            allResults.push(...await benchmarkRead(iterations, verbose));
        }
        if (!tool || tool === 'write') {
            allResults.push(...await benchmarkWrite(iterations, verbose));
        }
        if (!tool || tool === 'glob') {
            allResults.push(...await benchmarkGlob(iterations, verbose));
        }
        if (!tool || tool === 'grep') {
            allResults.push(...await benchmarkGrep(iterations, verbose));
        }
        if (!tool || tool === 'bash') {
            allResults.push(...await benchmarkBash(iterations, verbose));
        }

        // Print results
        const allPassed = printResults(allResults);

        // Cleanup
        if (verbose) console.log('\n[Cleanup] Removing test environment...');
        await cleanupTestEnvironment();

        // Summary
        if (allPassed) {
            console.log('\n✅ BENCHMARK PASSED: All operations within target thresholds\n');
            return true;
        } else {
            console.log('\n❌ BENCHMARK FAILED: Some operations exceeded target thresholds\n');
            return false;
        }
    } catch (error) {
        console.error('\n❌ Benchmark error:', error.message);
        if (verbose) console.error(error.stack);
        await cleanupTestEnvironment();
        return false;
    }
}

/**
 * Parse command line arguments
 */
function parseArgs() {
    const args = process.argv.slice(2);
    const options = {
        iterations: DEFAULT_ITERATIONS,
        tool: null,
        verbose: false
    };

    for (let i = 0; i < args.length; i++) {
        switch (args[i]) {
            case '--iterations':
                options.iterations = parseInt(args[++i], 10);
                break;
            case '--tool':
                options.tool = args[++i].toLowerCase();
                break;
            case '--verbose':
                options.verbose = true;
                break;
            case '--help':
                console.log(`
Claude Code Tools Performance Benchmark

Usage: node tests/claude-tools-benchmark.js [options]

Options:
  --iterations N    Number of iterations per test (default: ${DEFAULT_ITERATIONS})
  --tool <name>     Run only specific tool benchmark (read|write|glob|grep|bash)
  --verbose         Show detailed output
  --help            Show this help message

Examples:
  node tests/claude-tools-benchmark.js
  node tests/claude-tools-benchmark.js --iterations 500
  node tests/claude-tools-benchmark.js --tool read
  node tests/claude-tools-benchmark.js --verbose
                `);
                process.exit(0);
        }
    }

    return options;
}

// Run benchmarks
const options = parseArgs();
runBenchmarks(options).then(success => {
    process.exit(success ? 0 : 1);
}).catch(error => {
    console.error('Fatal error:', error);
    process.exit(1);
});

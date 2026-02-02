#!/usr/bin/env node

/**
 * Kiro Debug Dump Analyzer
 * Analyzes debug dumps from internal/debug/dumper.go
 */

import fs from 'fs';
import path from 'path';

class DumpAnalyzer {
  constructor(basePath = 'kiro-debug') {
    this.basePath = basePath;
    this.sessions = { errors: [], success: [] };
    this.stats = {
      total: 0,
      errors: 0,
      success: 0,
      errorTypes: {},
      models: {},
      accounts: {},
      timeRange: { start: null, end: null }
    };
  }

  /**
   * Discover all session directories
   */
  discoverSessions() {
    const errorsDir = path.join(this.basePath, 'errors');
    const successDir = path.join(this.basePath, 'success');

    if (fs.existsSync(errorsDir)) {
      this.sessions.errors = fs.readdirSync(errorsDir)
        .filter(name => fs.statSync(path.join(errorsDir, name)).isDirectory())
        .map(name => path.join(errorsDir, name));
    }

    if (fs.existsSync(successDir)) {
      this.sessions.success = fs.readdirSync(successDir)
        .filter(name => fs.statSync(path.join(successDir, name)).isDirectory())
        .map(name => path.join(successDir, name));
    }

    this.stats.total = this.sessions.errors.length + this.sessions.success.length;
    this.stats.errors = this.sessions.errors.length;
    this.stats.success = this.sessions.success.length;

    return this.sessions;
  }

  /**
   * Load session data from directory
   */
  loadSession(sessionDir) {
    const session = {
      dir: sessionDir,
      id: path.basename(sessionDir),
      files: {}
    };

    // Load metadata (required)
    const metadataPath = path.join(sessionDir, 'metadata.json');
    if (fs.existsSync(metadataPath)) {
      try {
        session.metadata = JSON.parse(fs.readFileSync(metadataPath, 'utf8'));
      } catch (err) {
        session.files.metadata = { error: err.message };
      }
    }

    // Load optional files
    const optionalFiles = [
      'request.json',
      'kiro_request.json',
      'response.json',
      'kiro_response.json'
    ];

    for (const file of optionalFiles) {
      const filePath = path.join(sessionDir, file);
      if (fs.existsSync(filePath)) {
        try {
          session.files[file] = JSON.parse(fs.readFileSync(filePath, 'utf8'));
        } catch (err) {
          session.files[file] = { error: err.message };
        }
      }
    }

    // Load JSONL files
    const jsonlFiles = ['kiro_chunks.jsonl', 'claude_chunks.jsonl'];
    for (const file of jsonlFiles) {
      const filePath = path.join(sessionDir, file);
      if (fs.existsSync(filePath)) {
        try {
          const content = fs.readFileSync(filePath, 'utf8');
          session.files[file] = content
            .split('\n')
            .filter(line => line.trim())
            .map(line => JSON.parse(line));
        } catch (err) {
          session.files[file] = { error: err.message };
        }
      }
    }

    return session;
  }

  /**
   * Analyze a single session
   */
  analyzeSession(session) {
    const analysis = {
      id: session.id,
      status: session.metadata?.success ? 'SUCCESS' : 'FAILED',
      error: session.metadata?.error,
      errorType: session.metadata?.error_type,
      exceptionPayload: session.metadata?.exception_payload,
      model: session.metadata?.model,
      account: session.metadata?.account_uuid,
      duration: null,
      chunks: {
        kiro: session.files['kiro_chunks.jsonl']?.length || 0,
        claude: session.files['claude_chunks.jsonl']?.length || 0
      },
      transformations: [],
      recommendations: []
    };

    // Calculate duration
    if (session.metadata?.start_time && session.metadata?.end_time) {
      const start = new Date(session.metadata.start_time);
      const end = new Date(session.metadata.end_time);
      analysis.duration = ((end - start) / 1000).toFixed(2) + 's';

      // Update time range
      if (!this.stats.timeRange.start || start < new Date(this.stats.timeRange.start)) {
        this.stats.timeRange.start = session.metadata.start_time;
      }
      if (!this.stats.timeRange.end || end > new Date(this.stats.timeRange.end)) {
        this.stats.timeRange.end = session.metadata.end_time;
      }
    }

    // Update statistics
    if (analysis.errorType) {
      this.stats.errorTypes[analysis.errorType] = (this.stats.errorTypes[analysis.errorType] || 0) + 1;
    }
    if (analysis.model) {
      this.stats.models[analysis.model] = (this.stats.models[analysis.model] || 0) + 1;
    }
    if (analysis.account) {
      if (!this.stats.accounts[analysis.account]) {
        this.stats.accounts[analysis.account] = { total: 0, failures: 0 };
      }
      this.stats.accounts[analysis.account].total++;
      if (!session.metadata?.success) {
        this.stats.accounts[analysis.account].failures++;
      }
    }

    // Analyze request transformation
    if (session.files['request.json'] && session.files['kiro_request.json']) {
      this.analyzeRequestTransformation(session, analysis);
    }

    // Analyze streaming
    if (analysis.chunks.kiro > 0 || analysis.chunks.claude > 0) {
      this.analyzeStreaming(session, analysis);
    }

    // Generate recommendations
    this.generateRecommendations(session, analysis);

    return analysis;
  }

  /**
   * Analyze request transformation
   */
  analyzeRequestTransformation(session, analysis) {
    const clientReq = session.files['request.json'];
    const kiroReq = session.files['kiro_request.json'];

    // Check for conversationState addition
    if (kiroReq.conversationState && !clientReq.conversationState) {
      analysis.transformations.push('Added conversationState wrapper');
    }

    // Check for model mapping
    const clientModel = clientReq.model;
    const kiroModel = kiroReq.conversationState?.currentMessage?.userInputMessage?.modelId;
    if (clientModel && kiroModel && clientModel !== kiroModel) {
      analysis.transformations.push(`Model mapping: ${clientModel} → ${kiroModel}`);
    }

    // Check prompt length
    const clientPrompt = JSON.stringify(clientReq.messages || clientReq);
    const kiroPrompt = kiroReq.conversationState?.currentMessage?.userInputMessage?.content;
    if (kiroPrompt) {
      analysis.transformations.push(`Prompt: ${clientPrompt.length} → ${kiroPrompt.length} chars`);
    }
  }

  /**
   * Analyze streaming chunks
   */
  analyzeStreaming(session, analysis) {
    const kiroChunks = session.files['kiro_chunks.jsonl'] || [];
    const claudeChunks = session.files['claude_chunks.jsonl'] || [];

    analysis.streaming = {
      kiroChunks: kiroChunks.length,
      claudeChunks: claudeChunks.length,
      started: false,
      completed: false,
      partialContent: '',
      exception: null
    };

    // Check if stream started
    const messageStart = claudeChunks.find(c => c.event === 'message_start');
    if (messageStart) {
      analysis.streaming.started = true;
      analysis.streaming.tokens = messageStart.data?.message?.usage;
    }

    // Check if stream completed
    const messageStop = claudeChunks.find(c => c.event === 'message_stop' || c.event === 'message_delta');
    if (messageStop) {
      analysis.streaming.completed = true;
    }

    // Extract partial content
    const contentDeltas = claudeChunks.filter(c => c.event === 'content_block_delta');
    if (contentDeltas.length > 0) {
      analysis.streaming.partialContent = contentDeltas
        .map(c => c.data?.delta?.text || '')
        .join('')
        .substring(0, 100); // First 100 chars
    }

    // Check for exception
    const exception = kiroChunks.find(c => c.exception || c.error);
    if (exception) {
      analysis.streaming.exception = exception;
    }
  }

  /**
   * Generate recommendations
   */
  generateRecommendations(session, analysis) {
    // Stream exception recommendations
    if (analysis.errorType === 'stream_exception') {
      analysis.recommendations.push('⚠️  Stream terminated unexpectedly - check Kiro API connection stability');

      if (analysis.chunks.kiro < 3) {
        analysis.recommendations.push('Stream failed early - possible connection issue');
      } else if (!analysis.streaming?.completed) {
        analysis.recommendations.push('Stream started but did not complete - possible timeout');
      }
    }

    // Rate limit recommendations
    if (analysis.errorType === 'rate_limit') {
      analysis.recommendations.push('⚠️  Rate limit hit - consider account rotation or backoff');
    }

    // Account health recommendations
    if (this.stats.accounts[analysis.account]?.failures > 3) {
      analysis.recommendations.push(`⚠️  Account ${analysis.account.substring(0, 8)}... has ${this.stats.accounts[analysis.account].failures} failures - consider health check`);
    }

    // Missing data recommendations
    if (!session.files['kiro_request.json']) {
      analysis.recommendations.push('Missing kiro_request.json - failure occurred before API call');
    }
    if (!session.files['kiro_chunks.jsonl']) {
      analysis.recommendations.push('Missing kiro_chunks.jsonl - no response received from API');
    }
  }

  /**
   * Print summary report
   */
  printSummary() {
    console.log('\n=== Kiro Debug Dump Analysis ===\n');
    console.log(`Directory: ${this.basePath}`);
    console.log(`Sessions: ${this.stats.total} (${this.stats.errors} errors, ${this.stats.success} success)`);

    if (this.stats.timeRange.start && this.stats.timeRange.end) {
      console.log(`Time Range: ${this.stats.timeRange.start} - ${this.stats.timeRange.end}`);
    }

    if (Object.keys(this.stats.errorTypes).length > 0) {
      console.log('\nError Breakdown:');
      const sortedErrors = Object.entries(this.stats.errorTypes)
        .sort((a, b) => b[1] - a[1]);
      for (const [type, count] of sortedErrors) {
        const pct = ((count / this.stats.errors) * 100).toFixed(1);
        console.log(`  - ${type}: ${count} (${pct}%)`);
      }
    }

    if (Object.keys(this.stats.models).length > 0) {
      console.log('\nTop Models:');
      const sortedModels = Object.entries(this.stats.models)
        .sort((a, b) => b[1] - a[1])
        .slice(0, 5);
      for (const [model, count] of sortedModels) {
        console.log(`  - ${model}: ${count} sessions`);
      }
    }

    if (Object.keys(this.stats.accounts).length > 0) {
      console.log('\nAccount Health:');
      const problematicAccounts = Object.entries(this.stats.accounts)
        .filter(([_, stats]) => stats.failures > 0)
        .sort((a, b) => b[1].failures - a[1].failures)
        .slice(0, 5);

      for (const [account, stats] of problematicAccounts) {
        const failRate = ((stats.failures / stats.total) * 100).toFixed(1);
        console.log(`  - ${account.substring(0, 8)}...: ${stats.failures}/${stats.total} failures (${failRate}%)`);
      }
    }
  }

  /**
   * Print session detail
   */
  printSessionDetail(analysis) {
    console.log(`\n--- Session: ${analysis.id} ---`);
    console.log(`Status: ${analysis.status}`);

    if (analysis.error) {
      console.log(`Error: ${analysis.error}`);
    }
    if (analysis.errorType) {
      console.log(`Error Type: ${analysis.errorType}`);
    }
    if (analysis.exceptionPayload) {
      try {
        const exception = JSON.parse(analysis.exceptionPayload);
        console.log(`Exception: ${JSON.stringify(exception, null, 2)}`);
      } catch {
        console.log(`Exception: ${analysis.exceptionPayload}`);
      }
    }
    if (analysis.model) {
      console.log(`Model: ${analysis.model}`);
    }
    if (analysis.account) {
      console.log(`Account: ${analysis.account}`);
    }
    if (analysis.duration) {
      console.log(`Duration: ${analysis.duration}`);
    }

    console.log(`Chunks: ${analysis.chunks.kiro} kiro, ${analysis.chunks.claude} claude`);

    if (analysis.transformations.length > 0) {
      console.log('\nRequest Transformation:');
      for (const transform of analysis.transformations) {
        console.log(`  - ${transform}`);
      }
    }

    if (analysis.streaming) {
      console.log('\nStream Analysis:');
      console.log(`  - Started: ${analysis.streaming.started ? 'Yes' : 'No'}`);
      console.log(`  - Completed: ${analysis.streaming.completed ? 'Yes' : 'No'}`);
      if (analysis.streaming.partialContent) {
        console.log(`  - Partial content: "${analysis.streaming.partialContent}${analysis.streaming.partialContent.length >= 100 ? '...' : ''}"`);
      }
      if (analysis.streaming.exception) {
        console.log(`  - Exception: ${JSON.stringify(analysis.streaming.exception)}`);
      }
      if (analysis.streaming.tokens) {
        console.log(`  - Tokens: ${JSON.stringify(analysis.streaming.tokens)}`);
      }
    }

    if (analysis.recommendations.length > 0) {
      console.log('\nRecommendations:');
      for (const rec of analysis.recommendations) {
        console.log(`  ${rec}`);
      }
    }
  }

  /**
   * Run analysis
   */
  async run(targetPath = null) {
    // If specific session directory provided
    if (targetPath && fs.statSync(targetPath).isFile() === false) {
      const metadataPath = path.join(targetPath, 'metadata.json');
      if (fs.existsSync(metadataPath)) {
        // Single session analysis
        const session = this.loadSession(targetPath);
        const analysis = this.analyzeSession(session);
        this.printSessionDetail(analysis);
        return;
      }
    }

    // Full analysis
    this.discoverSessions();

    if (this.stats.total === 0) {
      console.log(`No sessions found in ${this.basePath}`);
      return;
    }

    // Analyze all sessions
    const allAnalyses = [];

    for (const sessionDir of this.sessions.errors) {
      const session = this.loadSession(sessionDir);
      const analysis = this.analyzeSession(session);
      allAnalyses.push(analysis);
    }

    for (const sessionDir of this.sessions.success) {
      const session = this.loadSession(sessionDir);
      const analysis = this.analyzeSession(session);
      allAnalyses.push(analysis);
    }

    // Print summary
    this.printSummary();

    // Print error details
    if (this.stats.errors > 0) {
      console.log('\n=== Error Session Details ===');
      const errorAnalyses = allAnalyses.filter(a => a.status === 'FAILED');
      for (const analysis of errorAnalyses) {
        this.printSessionDetail(analysis);
      }
    }

    // Print success samples (if requested)
    if (this.stats.success > 0 && this.stats.success <= 3) {
      console.log('\n=== Success Session Samples ===');
      const successAnalyses = allAnalyses.filter(a => a.status === 'SUCCESS');
      for (const analysis of successAnalyses.slice(0, 3)) {
        this.printSessionDetail(analysis);
      }
    }
  }
}

// Main execution
const targetPath = process.argv[2] || 'kiro-debug';
const analyzer = new DumpAnalyzer(targetPath);
analyzer.run(targetPath).catch(console.error);

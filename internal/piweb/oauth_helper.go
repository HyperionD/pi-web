package piweb

const oauthHelperScript = `
import { createInterface } from 'node:readline';
import { chmodSync, existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname } from 'node:path';
import { pathToFileURL } from 'node:url';

const oauthModule = await import(pathToFileURL(process.env.PI_AI_OAUTH_INDEX).href);
const providerEntries = typeof oauthModule.builtinProviders === 'function'
  ? oauthModule.builtinProviders().filter((entry) => entry.auth?.oauth)
  : null;
const provider = (providerId) => providerEntries
  ? providerEntries.find((entry) => entry.id === providerId)?.auth.oauth
  : oauthModule.getOAuthProvider(providerId);
const providerInfo = (entry) => ({ id: entry.id, name: entry.name });
const providerId = process.argv[process.argv.length - 1];
const selectedProvider = provider(providerId);
const authPath = process.env.PI_AUTH_PATH;

function emit(event) {
  process.stdout.write(JSON.stringify(event) + '\n');
}

if (!selectedProvider) {
  emit({ type: 'error', message: 'Unknown OAuth provider: ' + providerId });
  process.exit(2);
}

const pending = [];
const input = createInterface({ input: process.stdin, crlfDelay: Infinity });
input.on('line', (line) => {
  let message;
  try { message = JSON.parse(line); } catch { return; }
  if (message.type === 'input') {
    const next = pending.shift();
    if (next) next(String(message.value ?? ''));
  }
});

function requestInput(kind, prompt) {
  emit({ type: kind, prompt });
  return new Promise((resolve) => pending.push(resolve));
}

function loadAuth() {
  if (!existsSync(authPath)) return {};
  try { return JSON.parse(readFileSync(authPath, 'utf-8')); } catch { return {}; }
}

function saveAuth(credentials) {
  mkdirSync(dirname(authPath), { recursive: true, mode: 0o700 });
  const auth = loadAuth();
  auth[providerId] = { type: 'oauth', ...credentials };
  writeFileSync(authPath, JSON.stringify(auth, null, 2), 'utf-8');
  chmodSync(authPath, 0o600);
}

function notify(event) {
  switch (event.type) {
    case 'auth_url':
      emit({ type: 'auth', url: event.url, instructions: event.instructions || '' });
      break;
    case 'device_code':
      emit({ type: 'auth', url: event.verificationUri, instructions: event.userCode ? 'Code: ' + event.userCode : '' });
      break;
    case 'info':
    case 'progress':
      if (event.message) emit({ type: 'progress', message: event.message });
      break;
  }
}

try {
  const entry = providerEntries?.find((item) => item.id === providerId);
  emit({ type: 'started', provider: providerEntries ? providerInfo(entry) : { id: providerId, name: selectedProvider.name } });
  const credentials = providerEntries
    ? await selectedProvider.login({
      signal: new AbortController().signal,
      prompt: async (prompt) => requestInput(prompt.type === 'manual_code' ? 'manualCode' : 'prompt', prompt),
      notify,
    })
    : await selectedProvider.login({
      onAuth: (info) => emit({ type: 'auth', url: info.url, instructions: info.instructions || '' }),
      onPrompt: async (prompt) => requestInput('prompt', prompt),
      onManualCodeInput: async () => requestInput('manualCode', { message: 'Paste authorization code or final redirect URL' }),
      onProgress: (message) => emit({ type: 'progress', message }),
    });
  saveAuth(credentials);
  emit({ type: 'success', provider: providerEntries ? providerInfo(entry) : { id: providerId, name: selectedProvider.name } });
  input.close();
  process.exit(0);
} catch (error) {
  emit({ type: 'error', message: error instanceof Error ? error.message : String(error) });
  input.close();
  process.exit(1);
}
`

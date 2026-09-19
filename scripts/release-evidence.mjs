import { execFileSync } from 'node:child_process';
import { mkdirSync, writeFileSync } from 'node:fs';
import { validateReleaseMetadata } from './ci-scope.mjs';

const context = JSON.parse(process.env.RELEASE_CONTEXT);
const git = (...args) => execFileSync('git', args, {encoding: 'utf8'}).trim();
if (!context.full || !context.release || git('rev-parse', 'HEAD') !== context.commit ||
    git('rev-parse', 'HEAD^{tree}') !== context.tree || validateReleaseMetadata() !== context.version) {
  throw new Error('release evidence does not match the validated checkout');
}
mkdirSync('.artifacts', {recursive: true});
writeFileSync('.artifacts/release-validation.json', JSON.stringify({
  format: 1, repository: process.env.GITHUB_REPOSITORY,
  run: Number(process.env.GITHUB_RUN_ID), attempt: Number(process.env.GITHUB_RUN_ATTEMPT), ...context,
}, null, 2) + '\n');

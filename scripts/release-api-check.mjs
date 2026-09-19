import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

export function unexpectedBreakingChanges(report) {
  const unexpected = [];
  let incompatible = false;
  for (const line of report.split(/\r?\n/).filter(Boolean)) {
    if (line === 'Incompatible changes:') { incompatible = true; continue; }
    if (line === 'Compatible changes:') { incompatible = false; continue; }
    if (!line.startsWith('- ')) throw new Error(`unrecognized API report line: ${line}`);
    if (!incompatible) continue;
    // Experimental packages do not carry the stable root API guarantee.
    if (/^- \.\/experimental\/v[12](?:[./:]|$)/.test(line)) continue;
    // Only schema provenance values may change without changing API types.
    if (/^- (SchemaArtifactVersion|SchemaCommit|SchemaTag): value changed from "[^"]*" to "[^"]*"$/.test(line)) continue;
    unexpected.push(line);
  }
  return unexpected;
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const breaking = unexpectedBreakingChanges(readFileSync(process.argv[2], 'utf8'));
  if (breaking.length) throw new Error(`unexpected stable API changes:\n${breaking.join('\n')}`);
}

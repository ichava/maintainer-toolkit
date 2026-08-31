#!/usr/bin/env node
//
// census.mjs — icon corpus fidelity census
//
// Walks every icon pack, parses every SVG, records which constructs are
// present, and (when a policy file is supplied) reports what each sanitiser
// allowlist would strip and what the user would see as a result.
//
// No dependencies. No running application required. Pure text analysis.
//
// usage:
//   node migration/census.mjs > migration/census.json
//   ICHAVA_ROOT=/path/to/ichava node migration/census.mjs > census.json
//   node migration/census.mjs --pretty            human-readable summary
//   node migration/census.mjs --policy path.json  explicit policy file
//
// Two modes:
//   DISCOVERY  no policy file → reports what constructs exist. Always works.
//   ANALYSIS   policy file present → also reports what each policy strips,
//              the visible outcome, and server/client divergence.
//
// Output is deterministic (sorted keys, no timestamps) so runs diff cleanly.
// Re-run after the allowlist change to prove the widening worked.

import { readdirSync, readFileSync, statSync, existsSync } from 'node:fs';
import { join, relative, extname, basename, dirname, resolve } from 'node:path';

// ---------------------------------------------------------------------------
// configuration
// ---------------------------------------------------------------------------

const ROOT = process.env.ICHAVA_ROOT || process.cwd();

const SKIP_DIRS = new Set([
  '.git', 'node_modules', 'vendor', '.idea', '.vscode', 'storage',
  'bootstrap', 'public/build', '.github', 'coverage', 'dist', '.next', 'migration',
]);

// directories to search for packs, relative to ROOT. first existing wins,
// but all are scanned so local packages/ and installed vendor/ both surface.
const PACK_ROOTS = [
  'packages',
  'vendor/ichava',
  'ichava',
  'resources',
  '.',
];

const WORST_SAMPLES_PER_PACK = 3;

// ---------------------------------------------------------------------------
// policy loading
//
// The allowlists MUST come from source. This script does not guess them.
// Create migration/policy.json shaped like the example printed by
// --policy-template, filling in the verbatim values from:
//   server: config/ichava.php  (ichava.svg.allowed_tags / allowed_attributes
//                               / forbidden_tags / forbidden_attributes)
//   client: the DOMPurify config (ALLOWED_TAGS / ALLOWED_ATTR /
//                                 FORBID_TAGS / FORBID_ATTR)
// ---------------------------------------------------------------------------

const POLICY_TEMPLATE = {
  _note: 'Fill from source. Do not guess. Empty arrays mean "not restricted".',
  server: {
    _source: 'config/ichava.php → ichava.svg.*',
    allowed_tags: [],
    allowed_attributes: [],
    forbidden_tags: [],
    forbidden_attributes: [],
  },
  client: {
    _source: 'DOMPurify config in the Vue app',
    ALLOWED_TAGS: [],
    ALLOWED_ATTR: [],
    FORBID_TAGS: [],
    FORBID_ATTR: [],
  },
};

function loadPolicy(explicitPath) {
  const candidates = explicitPath
    ? [explicitPath]
    : [
        join(ROOT, 'migration', 'policy.json'),
        join(ROOT, 'policy.json'),
        join(process.cwd(), 'policy.json'),
      ];
  for (const p of candidates) {
    if (!existsSync(p)) continue;
    try {
      const raw = JSON.parse(readFileSync(p, 'utf8'));
      const filled =
        (raw.server?.allowed_tags?.length || raw.server?.forbidden_tags?.length ||
         raw.server?.forbidden_attributes?.length) ||
        (raw.client?.ALLOWED_TAGS?.length || raw.client?.FORBID_TAGS?.length ||
         raw.client?.FORBID_ATTR?.length);
      return { policy: raw, path: p, usable: Boolean(filled) };
    } catch (e) {
      return { policy: null, path: p, usable: false, error: String(e.message) };
    }
  }
  return { policy: null, path: null, usable: false };
}

// ---------------------------------------------------------------------------
// minimal SVG scanner
//
// Not a full XML parser — deliberately. A census only needs to know which
// elements and attributes appear, and a dependency-free scanner is more
// portable than requiring an install. Comments, CDATA, DOCTYPE and
// processing instructions are skipped so their contents are not counted.
// ---------------------------------------------------------------------------

function scanSvg(text) {
  const elements = new Map();   // tagName -> count
  const attributes = new Map(); // attrName -> count
  const ids = [];
  const urlRefs = [];           // url(...) targets
  const hrefRefs = [];          // href / xlink:href values
  const styleValues = [];
  let rootAttrs = null;

  let i = 0;
  const n = text.length;

  while (i < n) {
    const lt = text.indexOf('<', i);
    if (lt === -1) break;

    // skip comments
    if (text.startsWith('<!--', lt)) {
      const end = text.indexOf('-->', lt + 4);
      i = end === -1 ? n : end + 3;
      continue;
    }
    // skip CDATA
    if (text.startsWith('<![CDATA[', lt)) {
      const end = text.indexOf(']]>', lt + 9);
      i = end === -1 ? n : end + 3;
      continue;
    }
    // skip DOCTYPE and other declarations
    if (text.startsWith('<!', lt)) {
      const end = text.indexOf('>', lt + 2);
      i = end === -1 ? n : end + 1;
      continue;
    }
    // skip processing instructions
    if (text.startsWith('<?', lt)) {
      const end = text.indexOf('?>', lt + 2);
      i = end === -1 ? n : end + 2;
      continue;
    }
    // closing tag
    if (text[lt + 1] === '/') {
      const end = text.indexOf('>', lt);
      i = end === -1 ? n : end + 1;
      continue;
    }

    // opening tag: find the terminating '>' that is not inside a quoted value
    let j = lt + 1;
    let quote = null;
    while (j < n) {
      const c = text[j];
      if (quote) {
        if (c === quote) quote = null;
      } else if (c === '"' || c === "'") {
        quote = c;
      } else if (c === '>') {
        break;
      }
      j++;
    }
    if (j >= n) break;

    const raw = text.slice(lt + 1, j);
    const m = /^([A-Za-z_][\w.:-]*)/.exec(raw);
    if (!m) { i = j + 1; continue; }

    const tag = m[1];
    const tagKey = tag.toLowerCase();
    elements.set(tagKey, (elements.get(tagKey) || 0) + 1);

    // attributes
    const attrRe = /([A-Za-z_][\w.:-]*)\s*=\s*("([^"]*)"|'([^']*)')/g;
    const attrsHere = {};
    let a;
    while ((a = attrRe.exec(raw)) !== null) {
      const name = a[1];
      const key = name.toLowerCase();
      const value = a[3] !== undefined ? a[3] : (a[4] !== undefined ? a[4] : '');
      attributes.set(key, (attributes.get(key) || 0) + 1);
      attrsHere[key] = value;

      if (key === 'id') ids.push(value);
      if (key === 'href' || key === 'xlink:href') hrefRefs.push(value);
      if (key === 'style') styleValues.push(value);

      let u;
      const urlRe = /url\(\s*(['"]?)([^)'"]*)\1\s*\)/g;
      while ((u = urlRe.exec(value)) !== null) urlRefs.push(u[2]);
    }

    if (rootAttrs === null && tagKey === 'svg') rootAttrs = attrsHere;

    i = j + 1;
  }

  // url() inside <style> element text
  const styleBlocks = [...text.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/gi)];
  for (const b of styleBlocks) {
    let u;
    const urlRe = /url\(\s*(['"]?)([^)'"]*)\1\s*\)/g;
    while ((u = urlRe.exec(b[1])) !== null) urlRefs.push(u[2]);
  }

  return { elements, attributes, ids, urlRefs, hrefRefs, styleValues, rootAttrs, styleBlocks: styleBlocks.length };
}

// ---------------------------------------------------------------------------
// classification
// ---------------------------------------------------------------------------

const PAINT_ATTRS = ['fill', 'stroke', 'stop-color', 'flood-color', 'lighting-color'];

function classifyPaintSource(scan, text) {
  const sources = [];
  const attrs = scan.attributes;

  if (/currentColor/i.test(text)) sources.push('currentColor');
  if (PAINT_ATTRS.some((a) => attrs.has(a))) sources.push('presentation-attribute');
  if (attrs.has('style') && scan.styleValues.some((v) => /fill|stroke|color/i.test(v))) {
    sources.push('style-attribute');
  }
  if (scan.styleBlocks > 0) sources.push('style-element');
  if (scan.elements.has('lineargradient') || scan.elements.has('radialgradient')) {
    sources.push('gradient');
  }
  if (scan.elements.has('image')) sources.push('image');
  if (scan.elements.has('use')) sources.push('use-reference');

  return sources.length ? sources.sort() : ['NONE-FOUND'];
}

function isFragmentRef(value) {
  return typeof value === 'string' && /^#[^\s]*$/.test(value.trim());
}

// ---------------------------------------------------------------------------
// policy application
// ---------------------------------------------------------------------------

function makePredicate(side, policy) {
  if (!policy) return null;

  const p = side === 'server'
    ? {
        allowTags: new Set((policy.server?.allowed_tags || []).map((s) => s.toLowerCase())),
        allowAttrs: new Set((policy.server?.allowed_attributes || []).map((s) => s.toLowerCase())),
        forbidTags: new Set((policy.server?.forbidden_tags || []).map((s) => s.toLowerCase())),
        forbidAttrs: new Set((policy.server?.forbidden_attributes || []).map((s) => s.toLowerCase())),
      }
    : {
        allowTags: new Set((policy.client?.ALLOWED_TAGS || []).map((s) => s.toLowerCase())),
        allowAttrs: new Set((policy.client?.ALLOWED_ATTR || []).map((s) => s.toLowerCase())),
        forbidTags: new Set((policy.client?.FORBID_TAGS || []).map((s) => s.toLowerCase())),
        forbidAttrs: new Set((policy.client?.FORBID_ATTR || []).map((s) => s.toLowerCase())),
      };

  return {
    tagStripped(tag) {
      const t = tag.toLowerCase();
      if (p.forbidTags.has(t)) return true;
      if (p.allowTags.size > 0 && !p.allowTags.has(t)) return true;
      return false;
    },
    attrStripped(attr) {
      const a = attr.toLowerCase();
      if (p.forbidAttrs.has(a)) return true;
      if (p.allowAttrs.size > 0 && !p.allowAttrs.has(a)) return true;
      return false;
    },
  };
}

function applyPolicy(scan, pred) {
  if (!pred) return null;
  const strippedTags = [];
  const strippedAttrs = [];
  for (const tag of scan.elements.keys()) if (pred.tagStripped(tag)) strippedTags.push(tag);
  for (const attr of scan.attributes.keys()) if (pred.attrStripped(attr)) strippedAttrs.push(attr);
  return { tags: strippedTags.sort(), attrs: strippedAttrs.sort() };
}

// Predicts what the user would SEE after sanitisation. This is the column
// that distinguishes "lost a decorative filter" from "renders solid black".
function predictRender(scan, paintSources, stripped) {
  if (!stripped) return null;
  const lostTag = (t) => stripped.tags.includes(t);
  const lostAttr = (a) => stripped.attrs.includes(a);

  const notes = [];
  // an icon can lose several things at once. `verdict` is the worst single
  // outcome for ranking; `losses` records every category so the matrix does
  // not hide, say, colour loss behind shape loss.
  const losses = new Set();
  let verdict = 'renders-correctly';
  const worsen = (v) => {
    if (severityRank(v) > severityRank(verdict)) verdict = v;
  };

  const usesUse = scan.elements.has('use');
  const usesGradient = scan.elements.has('lineargradient') || scan.elements.has('radialgradient');
  const usesMask = scan.elements.has('mask') || scan.elements.has('clippath');
  const refAttrLost = lostAttr('href') || lostAttr('xlink:href');

  // shape loss: content pulled in by reference disappears entirely
  if (usesUse && (lostTag('use') || refAttrLost)) {
    worsen('loses-shape');
    losses.add('shape:use-reference');
    notes.push('<use> reference removed — referenced content will not render');
  }

  // paint loss
  const onlyPaint = paintSources.length === 1 ? paintSources[0] : null;
  if (onlyPaint === 'style-attribute' && lostAttr('style')) {
    worsen('loses-colour');
    losses.add('colour:sole-paint-source');
    notes.push('style= is the ONLY paint source and is stripped — renders default black');
  } else if (lostAttr('style') && paintSources.includes('style-attribute')) {
    worsen('loses-colour');
    losses.add('colour:style-attribute');
    notes.push('style= stripped — some paint lost');
  }
  if (usesGradient && (lostTag('lineargradient') || lostTag('radialgradient') || refAttrLost)) {
    worsen('loses-colour');
    losses.add('colour:gradient');
    notes.push('gradient definition or reference removed');
  }
  if (onlyPaint === 'presentation-attribute' && (lostAttr('fill') || lostAttr('stroke'))) {
    worsen('loses-colour');
    losses.add('colour:sole-paint-source');
    notes.push('fill/stroke stripped — renders default black');
  }

  // structural
  if (usesMask && (lostTag('mask') || lostTag('clippath'))) {
    worsen('loses-shape');
    losses.add('shape:mask-clippath');
    notes.push('mask/clipPath removed — shape will be wrong');
  }
  if (lostTag('path') || lostTag('svg')) {
    worsen('renders-blank');
    losses.add('blank:core-element');
    notes.push('core drawing element removed');
  }

  // accessibility: invisible to sighted users, still a defect
  if (lostTag('title') || lostTag('desc')) {
    worsen('renders-correctly-a11y-loss');
    losses.add('a11y:accessible-name');
    notes.push('a11y: accessible name/description removed');
  }

  return { verdict, losses: [...losses].sort(), notes: notes.sort() };
}

function severityRank(verdict) {
  return {
    'renders-blank': 4,
    'loses-shape': 3,
    'loses-colour': 2,
    'renders-correctly-a11y-loss': 1,
    'renders-correctly': 0,
  }[verdict] ?? 0;
}

// ---------------------------------------------------------------------------
// filesystem walk
// ---------------------------------------------------------------------------

function walk(dir, out = [], depth = 0) {
  if (depth > 12) return out;
  let entries;
  try {
    entries = readdirSync(dir, { withFileTypes: true });
  } catch {
    return out;
  }
  for (const e of entries) {
    const full = join(dir, e.name);
    if (e.isDirectory()) {
      if (SKIP_DIRS.has(e.name)) continue;
      if (e.name.startsWith('.') && e.name !== '.') continue;
      walk(full, out, depth + 1);
    } else if (e.isFile()) {
      out.push(full);
    }
  }
  return out;
}

// A "pack" is the nearest ancestor directory containing composer.json or
// package.json. That matches how Laravel packages are laid out and avoids
// hardcoding a directory structure.
function findPackRoot(filePath, cache) {
  let dir = dirname(filePath);
  const seen = [];
  while (dir.startsWith(ROOT) && dir !== ROOT) {
    if (cache.has(dir)) {
      const v = cache.get(dir);
      for (const s of seen) cache.set(s, v);
      return v;
    }
    if (existsSync(join(dir, 'composer.json')) || existsSync(join(dir, 'package.json'))) {
      for (const s of seen) cache.set(s, dir);
      cache.set(dir, dir);
      return dir;
    }
    seen.push(dir);
    dir = dirname(dir);
  }
  for (const s of seen) cache.set(s, null);
  return null;
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

function main() {
  const args = process.argv.slice(2);
  const pretty = args.includes('--pretty');

  if (args.includes('--policy-template')) {
    process.stdout.write(JSON.stringify(POLICY_TEMPLATE, null, 2) + '\n');
    return;
  }

  const policyIdx = args.indexOf('--policy');
  const explicitPolicy = policyIdx !== -1 ? args[policyIdx + 1] : null;
  const { policy, path: policyPath, usable: policyUsable, error: policyError } =
    loadPolicy(explicitPolicy);

  const serverPred = policyUsable ? makePredicate('server', policy) : null;
  const clientPred = policyUsable ? makePredicate('client', policy) : null;

  // gather candidate directories
  const searchDirs = [];
  for (const r of PACK_ROOTS) {
    const p = resolve(ROOT, r);
    if (existsSync(p)) searchDirs.push(p);
  }
  if (searchDirs.length === 0) searchDirs.push(ROOT);

  const allFiles = new Set();
  for (const d of searchDirs) for (const f of walk(d)) allFiles.add(f);

  const packCache = new Map();
  const packs = new Map();

  for (const file of allFiles) {
    const ext = extname(file).toLowerCase();
    const packRoot = findPackRoot(file, packCache) || ROOT;
    const packName = relative(ROOT, packRoot) || basename(ROOT);

    if (!packs.has(packName)) {
      packs.set(packName, {
        pack: packName,
        path: relative(ROOT, packRoot) || '.',
        status: 'UNKNOWN',
        files: { total: 0, svg: 0, nonSvg: 0, bytes: 0, extensions: {} },
        icons: [],
      });
    }
    const P = packs.get(packName);
    P.files.total++;
    P.files.extensions[ext || '(none)'] = (P.files.extensions[ext || '(none)'] || 0) + 1;

    if (ext !== '.svg') { P.files.nonSvg++; continue; }

    let text, size;
    try {
      const st = statSync(file);
      size = st.size;
      text = readFileSync(file, 'utf8');
    } catch { continue; }

    P.files.svg++;
    P.files.bytes += size;

    const scan = scanSvg(text);
    const paint = classifyPaintSource(scan, text);
    const sStrip = applyPolicy(scan, serverPred);
    const cStrip = applyPolicy(scan, clientPred);

    const nonFragmentHrefs = scan.hrefRefs.filter((h) => !isFragmentRef(h));
    const nonFragmentUrls = scan.urlRefs.filter((u) => !isFragmentRef(u));

    P.icons.push({
      file: relative(ROOT, file),
      bytes: size,
      elements: Object.fromEntries([...scan.elements.entries()].sort()),
      attributes: Object.fromEntries([...scan.attributes.entries()].sort()),
      paintSources: paint,
      ids: scan.ids,
      hasViewBox: Boolean(scan.rootAttrs && scan.rootAttrs.viewbox),
      hardcodedSize: Boolean(scan.rootAttrs && (scan.rootAttrs.width || scan.rootAttrs.height)),
      styleElementCount: scan.styleBlocks,
      refs: {
        fragmentHrefs: scan.hrefRefs.filter(isFragmentRef).length,
        nonFragmentHrefs,
        fragmentUrls: scan.urlRefs.filter(isFragmentRef).length,
        nonFragmentUrls,
      },
      server: sStrip ? { stripped: sStrip, predicted: predictRender(scan, paint, sStrip) } : null,
      client: cStrip ? { stripped: cStrip, predicted: predictRender(scan, paint, cStrip) } : null,
    });
  }

  // summarise
  const packList = [];
  for (const P of [...packs.values()].sort((a, b) => a.pack.localeCompare(b.pack))) {
    if (P.files.svg === 0) {
      P.status = P.files.total === 0 ? 'NOT_INSTALLED' : 'NO_SVG_FILES';
    } else {
      P.status = 'SCANNED';
    }

    // construct matrix
    const constructs = {};
    const bump = (k) => { constructs[k] = (constructs[k] || 0) + 1; };
    const idMap = new Map();
    let affectedServer = 0, affectedClient = 0;
    const verdicts = {};
    const lossCategories = {};

    for (const ic of P.icons) {
      for (const el of Object.keys(ic.elements)) bump(`element:${el}`);
      for (const at of Object.keys(ic.attributes)) bump(`attribute:${at}`);
      for (const ps of ic.paintSources) bump(`paint:${ps}`);
      if (!ic.hasViewBox) bump('missing:viewBox');
      if (ic.hardcodedSize) bump('has:hardcodedSize');
      if (ic.refs.nonFragmentHrefs.length) bump('ref:non-fragment-href');
      if (ic.refs.nonFragmentUrls.length) bump('ref:non-fragment-url');

      for (const id of ic.ids) {
        if (!idMap.has(id)) idMap.set(id, []);
        idMap.get(id).push(ic.file);
      }

      if (ic.server?.predicted && ic.server.predicted.verdict !== 'renders-correctly') affectedServer++;
      if (ic.client?.predicted && ic.client.predicted.verdict !== 'renders-correctly') affectedClient++;
      const sv = ic.server?.predicted?.verdict, cv = ic.client?.predicted?.verdict;
      const v = severityRank(sv) >= severityRank(cv) ? sv : cv;
      if (v) verdicts[v] = (verdicts[v] || 0) + 1;
      for (const l of new Set([...(ic.server?.predicted?.losses || []), ...(ic.client?.predicted?.losses || [])])) {
        lossCategories[l] = (lossCategories[l] || 0) + 1;
      }
    }

    // id collisions across DIFFERENT files in the same pack
    const collisions = [];
    for (const [id, files] of idMap) {
      const unique = [...new Set(files)];
      if (unique.length > 1) collisions.push({ id, fileCount: unique.length, sample: unique.slice(0, 3).sort() });
    }
    collisions.sort((a, b) => b.fileCount - a.fileCount || a.id.localeCompare(b.id));

    // policy divergence
    const divergence = [];
    if (policyUsable) {
      const sTags = new Set(), cTags = new Set(), sAttrs = new Set(), cAttrs = new Set();
      for (const ic of P.icons) {
        ic.server?.stripped.tags.forEach((t) => sTags.add(t));
        ic.client?.stripped.tags.forEach((t) => cTags.add(t));
        ic.server?.stripped.attrs.forEach((a) => sAttrs.add(a));
        ic.client?.stripped.attrs.forEach((a) => cAttrs.add(a));
      }
      for (const t of [...sTags].filter((t) => !cTags.has(t))) divergence.push({ kind: 'tag', name: t, strippedBy: 'server-only' });
      for (const t of [...cTags].filter((t) => !sTags.has(t))) divergence.push({ kind: 'tag', name: t, strippedBy: 'client-only' });
      for (const a of [...sAttrs].filter((a) => !cAttrs.has(a))) divergence.push({ kind: 'attribute', name: a, strippedBy: 'server-only' });
      for (const a of [...cAttrs].filter((a) => !sAttrs.has(a))) divergence.push({ kind: 'attribute', name: a, strippedBy: 'client-only' });
      divergence.sort((a, b) => a.kind.localeCompare(b.kind) || a.name.localeCompare(b.name));
    }

    // worst affected samples, for fixture selection
    const worst = [...P.icons]
      .map((ic) => ({
        file: ic.file,
        serverVerdict: ic.server?.predicted?.verdict || null,
        clientVerdict: ic.client?.predicted?.verdict || null,
        rank: Math.max(
          severityRank(ic.server?.predicted?.verdict),
          severityRank(ic.client?.predicted?.verdict),
        ),
        worstVerdict:
          severityRank(ic.server?.predicted?.verdict) >= severityRank(ic.client?.predicted?.verdict)
            ? (ic.server?.predicted?.verdict || null)
            : (ic.client?.predicted?.verdict || null),
        losses: [...new Set([
          ...(ic.server?.predicted?.losses || []),
          ...(ic.client?.predicted?.losses || []),
        ])].sort(),
        notes: [...new Set([
          ...(ic.server?.predicted?.notes || []),
          ...(ic.client?.predicted?.notes || []),
        ])].sort(),
      }))
      .filter((w) => w.rank > 0)
      .sort((a, b) => b.rank - a.rank || a.file.localeCompare(b.file))
      .slice(0, WORST_SAMPLES_PER_PACK);

    packList.push({
      pack: P.pack,
      path: P.path,
      status: P.status,
      files: {
        ...P.files,
        extensions: Object.fromEntries(Object.entries(P.files.extensions).sort()),
      },
      constructs: Object.fromEntries(Object.entries(constructs).sort()),
      paintArchetypes: Object.fromEntries(
        Object.entries(constructs).filter(([k]) => k.startsWith('paint:')).sort()
      ),
      renderPrediction: policyUsable
        ? {
            affectedByServer: affectedServer,
            affectedByClient: affectedClient,
            pctAffectedClient: P.files.svg ? +((affectedClient / P.files.svg) * 100).toFixed(1) : 0,
            verdicts: Object.fromEntries(Object.entries(verdicts).sort()),
            lossCategories: Object.fromEntries(Object.entries(lossCategories).sort()),
          }
        : null,
      policyDivergence: policyUsable ? divergence : null,
      idCollisions: {
        total: collisions.length,
        worst: collisions.slice(0, 10),
      },
      worstAffectedSamples: policyUsable ? worst : null,
    });
  }

  const scanned = packList.filter((p) => p.status === 'SCANNED');
  const notInstalled = packList.filter((p) => p.status !== 'SCANNED');

  const report = {
    schemaVersion: 1,
    root: ROOT,
    mode: policyUsable ? 'ANALYSIS' : 'DISCOVERY',
    policy: {
      path: policyPath ? relative(ROOT, policyPath) : null,
      loaded: Boolean(policy),
      usable: policyUsable,
      error: policyError || null,
      note: policyUsable
        ? null
        : 'No usable policy file. Running DISCOVERY only — constructs are reported, '
        + 'but nothing is evaluated against the allowlists. Create migration/policy.json '
        + '(see --policy-template) with the VERBATIM values from source.',
    },
    totals: {
      packs: packList.length,
      packsScanned: scanned.length,
      packsNotInstalled: notInstalled.length,
      svgFiles: scanned.reduce((s, p) => s + p.files.svg, 0),
      bytes: scanned.reduce((s, p) => s + p.files.bytes, 0),
    },
    warnings: [
      ...notInstalled.map(
        (p) => `${p.pack}: ${p.status} — reported as NOT SCANNED, not as clean. `
             + `A pack with no SVGs cannot be assessed.`
      ),
      ...(policyUsable ? [] : ['Policy not loaded — no strip analysis performed.']),
    ].sort(),
    packs: packList,
  };

  if (pretty) {
    printPretty(report);
  } else {
    process.stdout.write(JSON.stringify(report, null, 2) + '\n');
  }
}

function printPretty(r) {
  const L = (s = '') => process.stdout.write(s + '\n');
  L();
  L('  ICON CORPUS FIDELITY CENSUS');
  L('  ' + '-'.repeat(70));
  L(`  root      ${r.root}`);
  L(`  mode      ${r.mode}`);
  L(`  packs     ${r.totals.packsScanned} scanned, ${r.totals.packsNotInstalled} not installed`);
  L(`  svg files ${r.totals.svgFiles}  (${(r.totals.bytes / 1048576).toFixed(1)} MB)`);
  L();
  if (r.warnings.length) {
    L('  WARNINGS');
    for (const w of r.warnings) L('    ! ' + w);
    L();
  }
  for (const p of r.packs) {
    L(`  ${p.pack}  [${p.status}]`);
    if (p.status !== 'SCANNED') {
      L(`      files: ${p.files.total}, extensions: ${JSON.stringify(p.files.extensions)}`);
      L();
      continue;
    }
    L(`      svg: ${p.files.svg}   bytes: ${(p.files.bytes / 1024).toFixed(0)}K`);
    const paints = Object.entries(p.paintArchetypes).map(([k, v]) => `${k.replace('paint:', '')}=${v}`);
    L(`      paint: ${paints.join('  ') || 'none'}`);
    if (p.renderPrediction) {
      L(`      affected: server ${p.renderPrediction.affectedByServer}, client ${p.renderPrediction.affectedByClient} (${p.renderPrediction.pctAffectedClient}%)`);
      const v = Object.entries(p.renderPrediction.verdicts).map(([k, n]) => `${k}=${n}`);
      if (v.length) L(`      verdicts: ${v.join('  ')}`);
    }
    if (p.idCollisions.total) {
      L(`      id collisions: ${p.idCollisions.total}  worst: ${p.idCollisions.worst.slice(0, 3).map((c) => `${c.id}(${c.fileCount})`).join(' ')}`);
    }
    if (p.policyDivergence?.length) {
      L(`      POLICY DIVERGENCE: ${p.policyDivergence.map((d) => `${d.name}[${d.strippedBy}]`).join(' ')}`);
    }
    if (p.worstAffectedSamples?.length) {
      L('      worst samples:');
      for (const w of p.worstAffectedSamples) {
        L(`        ${w.worstVerdict}  ${w.file}`);
        if (w.losses.length) L(`          losses: ${w.losses.join(', ')}`);
      }
    }
    L();
  }
}

main();

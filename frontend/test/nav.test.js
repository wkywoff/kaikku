import { test } from 'node:test';
import assert from 'node:assert/strict';
import { get } from 'svelte/store';

Object.defineProperty(global, 'navigator', {
  value: { userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1' },
  configurable: true,
});
global.matchMedia = () => ({ matches: true });

function fakeBrowser() {
  let entries = [{ state: null, url: '/' }];
  let index = 0;
  const listeners = new Set();
  const current = () => entries[index];
  const address = () => new URL(current().url, 'http://kaikku.test');
  const traverse = (delta) =>
    setTimeout(() => {
      if (!entries[index + delta]) return;
      index += delta;
      for (const listener of listeners) listener();
    });
  global.window = { addEventListener: (type, listener) => type === 'popstate' && listeners.add(listener) };
  global.location = {
    get pathname() {
      return address().pathname;
    },
    get search() {
      return address().search;
    },
  };
  global.history = {
    get state() {
      return structuredClone(current().state);
    },
    pushState(state, _title, url) {
      entries = [...entries.slice(0, index + 1), { state: structuredClone(state), url: url ?? current().url }];
      index++;
    },
    replaceState(state, _title, url) {
      entries[index] = { state: structuredClone(state), url: url ?? current().url };
    },
    go: traverse,
    back: () => traverse(-1),
    forward: () => traverse(1),
  };
  return {
    url: () => current().url,
    length: () => entries.length,
    canGoForward: () => index < entries.length - 1,
  };
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 30));

const iphone = fakeBrowser();
const iphoneGlobals = { window: global.window, location: global.location, history: global.history };
const iphoneNav = await import('../src/lib/stores/nav.js');
const { contextMenu, openMenu, closeMenu, closedByBack } = await import('../src/lib/stores/contextMenu.js');
const tap = { type: 'click', preventDefault() {}, stopPropagation() {} };

function onIphone(name, run) {
  test(`on iPhone ${name}`, async () => {
    Object.assign(global, iphoneGlobals);
    await run(iphoneNav);
  });
}

let copies = 0;
async function freshNav() {
  const browser = fakeBrowser();
  const nav = await import(`../src/lib/stores/nav.js?copy=${++copies}`);
  return { browser, nav };
}

test('"back" closes the window on top and leaves the page alone', async () => {
  const { browser, nav } = await freshNav();
  nav.navigate('/album/1');
  let open = true;
  nav.backCloses(() => (open = false));
  history.back();
  await settle();
  assert.equal(open, false);
  assert.equal(browser.url(), '/album/1');
  assert.equal(get(nav.navStore).id, '1');
});

test('a window closed by its own button takes its history entry along', async () => {
  const { browser, nav } = await freshNav();
  nav.navigate('/album/1');
  const closed = nav.backCloses(() => assert.fail('closed by "back"'));
  closed();
  await settle();
  assert.equal(history.state.overlay, undefined);
  history.back();
  await settle();
  assert.equal(browser.url(), '/');
  assert.equal(get(nav.navStore).view, 'home');
});

test('a page that must not be left stays on "back" and on "forward"', async () => {
  const { browser, nav } = await freshNav();
  nav.navigate('/album/1');
  nav.navigate('/album/2');
  nav.navigate('/album/3');
  history.back();
  await settle();
  const release = nav.guardLeave(() => false);
  history.back();
  await settle();
  assert.equal(browser.url(), '/album/2');
  history.forward();
  await settle();
  assert.equal(browser.url(), '/album/2');
  assert.equal(get(nav.navStore).id, '2');
  release();
  history.forward();
  await settle();
  assert.equal(browser.url(), '/album/3');
  assert.equal(get(nav.navStore).id, '3');
});

onIphone('a menu opened and closed leaves nothing to swipe forward to', async (nav) => {
  nav.navigate('/album/1');
  const entries = iphone.length();
  openMenu(tap, [{ label: 'Share' }]);
  assert.ok(get(contextMenu).sheet);
  closeMenu();
  await settle();
  assert.equal(get(contextMenu), null);
  assert.equal(iphone.canGoForward(), false);
  assert.equal(iphone.length(), entries);
});

onIphone('"back" over the player closes the menu first, then the player', async (nav) => {
  nav.navigate('/album/2');
  let player = true;
  nav.backCloses(() => (player = false));
  openMenu(tap, [{ label: 'Share' }]);
  history.back();
  await settle();
  assert.equal(get(contextMenu), null);
  assert.equal(closedByBack(), true);
  assert.equal(player, true);
  assert.equal(iphone.canGoForward(), false);
  history.back();
  await settle();
  assert.equal(player, false);
  assert.equal(iphone.url(), '/album/2');
});

onIphone('"back" over a page closes the menu and stays on the page', async (nav) => {
  nav.navigate('/album/3');
  openMenu(tap, [{ label: 'Share' }]);
  history.back();
  await settle();
  assert.equal(get(contextMenu), null);
  assert.equal(iphone.url(), '/album/3');
  assert.equal(get(nav.navStore).id, '3');
});

onIphone('"forward" with the menu open closes it and goes on', async (nav) => {
  nav.navigate('/album/4');
  nav.navigate('/album/5');
  history.back();
  await settle();
  openMenu(tap, [{ label: 'Share' }]);
  history.forward();
  await settle();
  assert.equal(get(contextMenu), null);
  assert.equal(iphone.url(), '/album/5');
  assert.equal(get(nav.navStore).id, '5');
});

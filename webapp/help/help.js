// The help center's pages (/help/): the topics drawer on small screens,
// links to headings, checklists to tick off, search across all articles,
// "On this page" following the reading position, the visualizations'
// switches, and "Ask an AI" (copying the help center for an assistant). The only thing kept is the sidebar's scroll position, for this
// tab (sessionStorage); checklists start afresh. Not part of the installed
// app.
(() => {
    'use strict';

    document.documentElement.classList.add('js');

    const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

    // --- Topics drawer (phones) ---------------------------------------------

    const setupMenu = () => {
        const button = document.querySelector('[data-menu]');
        const scrim = document.querySelector('[data-menu-close]');
        const nav = document.getElementById('hc-nav');
        if (!button || !nav) return;
        const set = (open) => {
            document.body.classList.toggle('nav-open', open);
            button.setAttribute('aria-expanded', String(open));
            if (scrim) scrim.hidden = !open;
            if (open) (nav.querySelector('[aria-current="page"]') || nav.querySelector('a'))?.focus({ preventScroll: true });
        };
        button.addEventListener('click', () => set(!document.body.classList.contains('nav-open')));
        scrim?.addEventListener('click', () => set(false));
        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape' && document.body.classList.contains('nav-open')) { set(false); button.focus(); }
        });
    };

    // --- Sidebar position ---------------------------------------------------

    // The sidebar keeps where it was scrolled to from page to page (for this
    // tab only), so the links stay where they were. It is placed as soon as
    // it is parsed, before it is first drawn, so nothing jumps. It only
    // moves when the current article would be out of sight (from search or
    // Previous/Next), and then just far enough; on a first visit it centres
    // it. Only the sidebar scrolls, never the page.
    const navKey = 'resound.help.navScroll';

    const placeNav = (nav) => {
        let saved = null;
        try { saved = sessionStorage.getItem(navKey); } catch (e) { /* storage blocked */ }
        if (saved !== null) nav.scrollTop = Number(saved) || 0;
        const current = nav.querySelector('[aria-current="page"]');
        if (!current) return;
        const view = nav.getBoundingClientRect();
        const box = current.getBoundingClientRect();
        const offset = box.top - view.top + nav.scrollTop; // within the sidebar's content
        if (saved === null) {
            nav.scrollTop = offset - (nav.clientHeight - box.height) / 2;
        } else if (box.top < view.top) {
            nav.scrollTop = offset - 16;
        } else if (box.bottom > view.bottom) {
            nav.scrollTop = offset + box.height - nav.clientHeight + 16;
        }
    };

    // watchNav places the sidebar the moment its last part is parsed: this
    // script runs in <head>, and mutation callbacks run before the browser
    // draws what was parsed.
    const watchNav = () => {
        let placed = false;
        const ready = () => {
            const nav = document.getElementById('hc-nav');
            if (placed || !nav || !nav.nextElementSibling) return placed;
            placed = true;
            placeNav(nav);
            return true;
        };
        if (ready()) return;
        const observer = new MutationObserver(() => { if (ready()) observer.disconnect(); });
        observer.observe(document.documentElement, { childList: true, subtree: true });
        document.addEventListener('DOMContentLoaded', () => { observer.disconnect(); ready(); }, { once: true });
    };

    const saveNav = () => {
        const nav = document.getElementById('hc-nav');
        if (!nav) return;
        try { sessionStorage.setItem(navKey, String(Math.round(nav.scrollTop))); } catch (e) { /* storage blocked */ }
    };
    window.addEventListener('pagehide', saveNav);
    document.addEventListener('click', (e) => { if (e.target.closest && e.target.closest('#hc-nav a')) saveNav(); });
    watchNav();

    // --- Headings: a "#" link to each ---------------------------------------

    const setupAnchors = () => {
        document.querySelectorAll('.hc-article h2[id], .hc-article h3[id]').forEach((h) => {
            if (h.closest('.hc-card')) return;
            const a = document.createElement('a');
            a.className = 'hc-anchor';
            a.href = '#' + h.id;
            a.textContent = '#';
            a.setAttribute('aria-label', 'Link to this section');
            h.prepend(a);
        });
    };

    // --- Checklists ---------------------------------------------------------

    const setupChecklists = () => {
        document.querySelectorAll('ul.checklist').forEach((list) => {
            const items = [...list.children].filter((li) => li.querySelector(':scope > input[type="checkbox"], :scope > p > input[type="checkbox"]'));
            if (!items.length) return;
            const status = document.createElement('div');
            status.className = 'checklist-status';
            const bar = document.createElement('span');
            bar.className = 'bar';
            bar.append(document.createElement('i'));
            const count = document.createElement('span');
            const reset = document.createElement('button');
            reset.type = 'button';
            reset.textContent = 'Start over';
            reset.hidden = true;
            status.append(bar, count, reset);
            list.after(status);

            const boxes = items.map((li) => {
                const box = li.querySelector('input[type="checkbox"]');
                box.disabled = false;
                box.checked = false;
                box.setAttribute('aria-label', 'Checked: ' + li.textContent.trim().slice(0, 80));
                // The input sits first in the item (or in its paragraph); keep it there.
                if (box.parentElement !== li) li.prepend(box);
                return box;
            });
            const update = () => {
                const done = boxes.filter((b) => b.checked).length;
                items.forEach((li, i) => li.classList.toggle('done', boxes[i].checked));
                count.textContent = done === boxes.length ? 'All checked' : `${done} of ${boxes.length} checked`;
                bar.firstChild.style.width = (100 * done / boxes.length) + '%';
                reset.hidden = done === 0;
            };
            items.forEach((li, i) => {
                li.addEventListener('click', (e) => {
                    if (e.target === boxes[i] || e.target.closest('a, button, kbd, code')) return;
                    if (window.getSelection && String(window.getSelection())) return; // selecting text
                    boxes[i].checked = !boxes[i].checked;
                    update();
                });
                boxes[i].addEventListener('change', update);
            });
            reset.addEventListener('click', () => { boxes.forEach((b) => { b.checked = false; }); update(); });
            update();
        });
    };

    // --- On this page -------------------------------------------------------

    const setupToc = () => {
        const links = [...document.querySelectorAll('.hc-toc a')];
        if (!links.length || !('IntersectionObserver' in window)) return;
        const byId = new Map(links.map((a) => [decodeURIComponent(a.hash.slice(1)), a]));
        const headings = [...byId.keys()].map((id) => document.getElementById(id)).filter(Boolean);
        const visible = new Set();
        const mark = () => {
            // The first heading in view, else the last one above the view.
            let current = headings.find((h) => visible.has(h));
            if (!current) current = headings.filter((h) => h.getBoundingClientRect().top < 0).pop() || headings[0];
            links.forEach((a) => a.classList.toggle('active', byId.get(current.id) === a));
        };
        const io = new IntersectionObserver((entries) => {
            entries.forEach((e) => (e.isIntersecting ? visible.add(e.target) : visible.delete(e.target)));
            mark();
        }, { rootMargin: '-70px 0px -55% 0px' });
        headings.forEach((h) => io.observe(h));
    };

    // --- Search -------------------------------------------------------------

    // fold makes text comparable: lower case, without accents.
    const fold = (s) => s.toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '');
    const escapeHTML = (s) => s.replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));

    const setupSearch = () => {
        const input = document.getElementById('hc-search');
        const box = document.getElementById('hc-results');
        if (!input || !box) return;
        let index = null;
        let loading = null;
        let selected = -1;

        const load = () => {
            loading ??= fetch('/help/search.json')
                .then((r) => (r.ok ? r.json() : Promise.reject(new Error(r.status))))
                .then((list) => {
                    index = list.map((p) => ({
                        ...p,
                        fTitle: fold(p.Title),
                        fHeads: (p.Headings || []).map((h) => ({ ...h, f: fold(h.Text) })),
                        fText: fold(p.Text),
                    }));
                })
                .catch(() => { index = []; });
            return loading;
        };

        // score ranks an article for the query's words: all must appear
        // somewhere; titles count most, then the opening paragraph (what the
        // article is about), headings, and how often the text has it.
        const score = (p, words) => {
            let total = 0;
            for (const w of words) {
                let s = 0;
                if (p.fTitle.includes(w)) s += p.fTitle.split(/\W+/).some((t) => t.startsWith(w)) ? 12 : 8;
                if (p.fText.slice(0, 300).includes(w)) s += 4;
                if (p.fHeads.some((h) => h.f.includes(w))) s += 3;
                if (p.fText.includes(w)) s += 1 + Math.min(4, p.fText.split(w).length - 1) * 0.5;
                if (!s) return 0;
                total += s;
            }
            return total;
        };

        // snippet is the text around the first word found, with the words marked.
        const snippet = (p, words) => {
            const at = Math.min(...words.map((w) => p.fText.indexOf(w)).filter((i) => i >= 0), Infinity);
            const start = at === Infinity ? 0 : Math.max(0, at - 50);
            let text = p.Text.slice(start, start + 140);
            if (start > 0) text = '…' + text.slice(text.indexOf(' ') + 1);
            if (start + 140 < p.Text.length) text += '…';
            let html = escapeHTML(text);
            for (const w of words) {
                if (w.length < 2) continue;
                const re = new RegExp('(' + w.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + ')', 'ig');
                html = html.replace(re, '<mark>$1</mark>');
            }
            return html;
        };

        const close = () => {
            box.hidden = true;
            input.setAttribute('aria-expanded', 'false');
            selected = -1;
        };

        const select = (i) => {
            const items = [...box.querySelectorAll('.hc-result')];
            if (!items.length) return;
            selected = (i + items.length) % items.length;
            items.forEach((el, j) => el.setAttribute('aria-selected', String(j === selected)));
            items[selected].scrollIntoView({ block: 'nearest' });
        };

        const show = async () => {
            const q = fold(input.value.trim());
            if (!q) { close(); return; }
            await load();
            if (fold(input.value.trim()) !== q) return; // typed on meanwhile
            const words = q.split(/\s+/).filter(Boolean);
            const hits = index
                .map((p) => ({ p, s: score(p, words) }))
                .filter((h) => h.s > 0)
                .sort((a, b) => b.s - a.s)
                .slice(0, 8);
            box.textContent = '';
            if (!hits.length) {
                const none = document.createElement('p');
                none.className = 'hc-noresult';
                none.textContent = 'Nothing found. Try another word, or browse the topics.';
                box.append(none);
            }
            for (const { p } of hits) {
                // Jump to the heading that matches, if one does.
                const head = p.fHeads.find((h) => words.every((w) => h.f.includes(w))) || p.fHeads.find((h) => words.some((w) => w.length > 2 && h.f.includes(w)));
                const a = document.createElement('a');
                a.className = 'hc-result';
                a.href = '/help/' + p.Slug + (head ? '#' + head.ID : '');
                a.setAttribute('role', 'option');
                a.innerHTML = `<small>${escapeHTML(p.Section)}</small><strong>${escapeHTML(p.Title)}${head ? ' › ' + escapeHTML(head.Text) : ''}</strong><span>${snippet(p, words)}</span>`;
                box.append(a);
            }
            selected = -1;
            box.hidden = false;
            input.setAttribute('aria-expanded', 'true');
        };

        input.addEventListener('focus', load, { once: true });
        input.addEventListener('input', show);
        input.addEventListener('keydown', (e) => {
            if (e.key === 'ArrowDown') { e.preventDefault(); if (box.hidden) show(); else select(selected + 1); }
            else if (e.key === 'ArrowUp') { e.preventDefault(); select(selected - 1); }
            else if (e.key === 'Enter') {
                const items = box.querySelectorAll('.hc-result');
                const target = items[Math.max(0, selected)];
                if (target) { e.preventDefault(); location.href = target.href; }
            } else if (e.key === 'Escape') {
                if (!box.hidden) close(); else input.blur();
            }
        });
        document.addEventListener('click', (e) => {
            if (!e.target.closest('.hc-search')) close();
        });
        // "/" focuses the search, as on most documentation sites.
        document.addEventListener('keydown', (e) => {
            if (e.key !== '/' || e.ctrlKey || e.metaKey || e.altKey) return;
            const t = e.target;
            if (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName)) return;
            e.preventDefault();
            input.focus();
        });
    };

    // --- Visualizations with modes (data-viz-modes) --------------------------

    // A switch sets data-mode; until it is used, the modes take turns by
    // themselves while the picture is on screen.
    const setupVizModes = () => {
        document.querySelectorAll('[data-viz-modes]').forEach((viz) => {
            const modes = viz.dataset.vizModes.split(/\s+/);
            const buttons = [...viz.querySelectorAll('[data-mode-set]')];
            const set = (mode) => {
                viz.dataset.mode = mode;
                buttons.forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.modeSet === mode)));
            };
            let timer = null;
            const stop = () => { clearInterval(timer); timer = null; };
            buttons.forEach((b) => b.addEventListener('click', () => { stop(); viz.dataset.touched = '1'; set(b.dataset.modeSet); }));
            if (reducedMotion || !('IntersectionObserver' in window)) return;
            new IntersectionObserver(([e]) => {
                if (viz.dataset.touched) return;
                if (e.isIntersecting && !timer) {
                    timer = setInterval(() => set(modes[(modes.indexOf(viz.dataset.mode) + 1) % modes.length]), 3200);
                } else if (!e.isIntersecting) stop();
            }, { threshold: 0.5 }).observe(viz);
        });
    };

    // --- Ask an AI ------------------------------------------------------------

    // "Copy for your AI" copies ai-context.txt: the prompt (ai-prompt.txt)
    // and every article as Markdown. The assistants' links are plain links
    // (llms.go). Those with the prompt in their address read the help from
    // the site themselves, and get it on the clipboard too, in case they
    // can't. The others open empty, ready for a paste. Browsers allow copying
    // only during the click, so the text is fetched as soon as a card is
    // near; until it has arrived, a ClipboardItem takes the pending text.
    const aiContext = '/help/ai-context.txt';

    const setupAI = () => {
        const cards = [...document.querySelectorAll('[data-ai]')];
        if (!cards.length) return;
        const texts = new Map(); // url → { promise, text }
        const get = (url) => {
            let t = texts.get(url);
            if (!t) {
                t = { text: null };
                t.promise = fetch(url)
                    .then((r) => (r.ok ? r.text() : Promise.reject(new Error(r.status))))
                    .then((text) => { t.text = text; return text; });
                t.promise.catch(() => texts.delete(url)); // try again next time
                texts.set(url, t);
            }
            return t;
        };
        const copy = async (url) => {
            if (!navigator.clipboard) throw new Error('no clipboard');
            const t = get(url);
            if (t.text !== null) return navigator.clipboard.writeText(t.text);
            if (window.ClipboardItem) {
                try {
                    const blob = t.promise.then((text) => new Blob([text], { type: 'text/plain' }));
                    return await navigator.clipboard.write([new ClipboardItem({ 'text/plain': blob })]);
                } catch (e) { /* not with a pending text: wait for it */ }
            }
            return navigator.clipboard.writeText(await t.promise);
        };

        const apple = /Mac|iPhone|iPad|iPod/.test(navigator.platform || navigator.userAgent);
        const paste = () => (window.matchMedia('(pointer: coarse)').matches ? 'touch and hold, then Paste' : (apple ? '⌘V' : 'Ctrl+V'));

        const status = (card, text, tone) => {
            const el = card.querySelector('[data-ai-status]');
            if (!el) return;
            el.dataset.tone = tone || '';
            el.textContent = text;
        };
        const flash = (button, text) => {
            const label = button.querySelector('[data-ai-copy-label]') || button;
            label.dataset.label ??= label.textContent;
            label.textContent = text;
            button.dataset.state = 'done';
            clearTimeout(button.flashTimer);
            button.flashTimer = setTimeout(() => { label.textContent = label.dataset.label; delete button.dataset.state; }, 2500);
        };
        const failed = (card) => status(card, "Your browser didn't allow copying. Try again, or in another browser.", 'error');

        // Fetch ahead once a card is close to the screen.
        const prefetch = () => get(aiContext);
        if ('IntersectionObserver' in window) {
            const io = new IntersectionObserver((entries) => {
                if (entries.some((e) => e.isIntersecting)) { prefetch(); io.disconnect(); }
            }, { rootMargin: '600px 0px' });
            cards.forEach((c) => io.observe(c));
        } else prefetch();

        cards.forEach((card) => {
            card.addEventListener('pointerenter', prefetch, { once: true });
            card.addEventListener('focusin', prefetch, { once: true });

            card.querySelector('[data-ai-copy]')?.addEventListener('click', async (e) => {
                const button = e.currentTarget;
                try {
                    await copy(aiContext);
                    flash(button, 'Copied');
                    status(card, `Copied all of Resound's help, with instructions for the AI. Paste it into any AI chat (${paste()}), then ask your question.`);
                } catch (err) {
                    failed(card);
                }
            });

            const page = card.querySelector('[data-ai-copy-page]');
            page?.addEventListener('click', async () => {
                try {
                    await copy(page.dataset.aiCopyPage);
                    flash(page, 'Copied');
                    status(card, 'Copied this article as Markdown.');
                } catch (err) {
                    failed(card);
                }
            });

            // The menu of assistants closes on a choice, a click elsewhere or Esc.
            const menu = card.querySelector('[data-ai-menu]');
            if (menu) {
                const closeMenu = (focus) => {
                    if (!menu.open) return;
                    menu.open = false;
                    if (focus) menu.querySelector('summary').focus();
                };
                document.addEventListener('click', (e) => { if (!menu.contains(e.target)) closeMenu(false); });
                menu.addEventListener('keydown', (e) => { if (e.key === 'Escape') { e.stopPropagation(); closeMenu(true); } });
            }

            // The link opens the assistant in a new tab by itself; this only copies.
            card.querySelectorAll('[data-ai-open]').forEach((a) => {
                a.addEventListener('click', async () => {
                    if (menu) menu.open = false;
                    const name = a.dataset.aiOpen;
                    const pasteOnly = a.hasAttribute('data-ai-paste');
                    try {
                        await copy(aiContext);
                        status(card, pasteOnly
                            ? `Copied. Paste it into ${name} (${paste()}), then ask your question.`
                            : `${name} opens with the instructions and reads the help from getresound.app. If it can't, paste it (${paste()}): it's on your clipboard.`);
                    } catch (err) {
                        if (pasteOnly) failed(card);
                        else status(card, `${name} opens with the instructions and reads the help from getresound.app.`);
                    }
                });
            });
        });
    };

    document.addEventListener('DOMContentLoaded', () => {
        setupAI();
        setupMenu();
        setupAnchors();
        setupChecklists();
        setupToc();
        setupSearch();
        setupVizModes();
    });
})();

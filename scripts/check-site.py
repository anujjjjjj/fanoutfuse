#!/usr/bin/env python3
"""Check recorded output and local assets; optionally exercise the page in a browser.

Run: python3 scripts/check-site.py [--browser]
Browser checks require the Python playwright package and an installed Chromium.
Set BROWSER_EXECUTABLE to use a browser outside the default installation.
"""
import argparse
import os
from functools import partial
from html.parser import HTMLParser
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import threading

ROOT = Path(__file__).resolve().parents[1]


class Page(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.capture = None
        self.demos = {}
        self.links = []
        self.assets = []

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == 'code' and attrs.get('id', '').startswith('demo-'):
            self.capture = attrs['id']
            self.demos[self.capture] = ''
        if tag == 'a':
            self.links.append(attrs.get('href', ''))
        if tag == 'script' or tag == 'link':
            self.assets.append(attrs.get('src') or attrs.get('href'))

    def handle_endtag(self, tag):
        if tag == 'code':
            self.capture = None

    def handle_data(self, data):
        if self.capture:
            self.demos[self.capture] += data


def check_content():
    page = Page()
    page.feed((ROOT / 'site/index.html').read_text())
    demo = (ROOT / 'docs/demo.txt').read_text()
    expected = {
        'demo-replay': demo[demo.index('$ fanoutfuse replay fixtures/69332.yaml'):demo.index('$ fanoutfuse replay fixtures/68619-burst.yaml')].rstrip(),
        'demo-pm': demo[demo.index('$ fanoutfuse pm\n'):demo.index('\n## pm --md')].rstrip(),
    }
    assert page.demos == expected, 'Recorded output must match docs/demo.txt verbatim'
    for issue in [69332, 68619, 77361, 77414, 83195]:
        assert any(link.endswith('/issues/' + str(issue)) for link in page.links)
    for asset in page.assets:
        assert asset and '://' not in asset, 'Assets must remain local'
        assert (ROOT / 'site' / asset).is_file(), asset
    assert (ROOT / 'site/app.js').is_file()
    print('PASS: verbatim demo output, five evidence links, local assets')


class QuietHandler(SimpleHTTPRequestHandler):
    def log_message(self, *_args):
        pass


def check_browser():
    from playwright.sync_api import sync_playwright
    server = ThreadingHTTPServer(('127.0.0.1', 0), partial(QuietHandler, directory=str(ROOT / 'site')))
    threading.Thread(target=server.serve_forever, daemon=True).start()
    try:
        with sync_playwright() as p:
            browser = p.chromium.launch(executable_path=os.environ.get("BROWSER_EXECUTABLE"))
            for width in [390, 1440]:
                page = browser.new_page(viewport={'width': width, 'height': 1000}, reduced_motion='reduce')
                errors = []
                page.on('pageerror', lambda error: errors.append(str(error)))
                page.goto(f'http://127.0.0.1:{server.server_port}', wait_until='networkidle')
                assert page.locator('#instrument').evaluate('(el) => el.classList.contains("tripped")')
                assert page.locator('#count').inner_text() == '127'
                assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), f'Overflow at {width}'
                page.get_by_role('button', name='Replay', exact=False).click()
                assert page.locator('#count').inner_text() == '127', 'Reduced motion must remain static'
                question = page.locator('summary').first
                question.click()
                assert page.locator('details').first.get_attribute('open') is not None
                page.screenshot(path=f'/tmp/fanoutfuse-site-{width}.png', full_page=True)
                page.emulate_media(reduced_motion='no-preference')
                page.locator('#replay-button').click()
                page.wait_for_function('document.querySelector("#instrument").classList.contains("warning")')
                page.wait_for_function('document.querySelector("#instrument").classList.contains("danger")')
                page.wait_for_function('document.querySelector("#instrument").classList.contains("tripped")', timeout=12000)
                assert page.locator('#nodes circle').count() == 127
                assert page.locator('#edges path').count() == 126
                assert page.locator('#trip-status').inner_text() == 'TRIPPED spawn_burst 15/10s > 15 · contained'
                assert not errors, errors
                page.close()
                print(f'PASS: {width}px layout, reduced motion, replay stages, tree, FAQ, no script errors')
            browser.close()
    finally:
        server.shutdown()
        server.server_close()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--browser', action='store_true')
    args = parser.parse_args()
    check_content()
    if args.browser:
        check_browser()

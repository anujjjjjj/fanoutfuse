(() => {
  'use strict';
  const panel = document.querySelector('#instrument');
  const nodes = document.querySelector('#nodes');
  const edges = document.querySelector('#edges');
  const count = document.querySelector('#count');
  const rate = document.querySelector('#rate');
  const phase = document.querySelector('#phase');
  const sequence = document.querySelector('#sequence');
  const status = document.querySelector('#trip-status');
  const elapsed = document.querySelector('#elapsed');
  const spark = document.querySelector('#spark');
  const button = document.querySelector('#replay-button');
  const motion = window.matchMedia('(prefers-reduced-motion: reduce)');
  const ns = 'http://www.w3.org/2000/svg';
  const points = [];
  for (let i = 0; i < 127; i++) {
    const depth = Math.floor(Math.log2(i + 1));
    const slot = i - (2 ** depth - 1);
    points.push({x: 32 + depth * 86, y: 18 + (slot + 0.5) * 276 / 2 ** depth});
  }
  let visible = 0;
  let frame = 0;
  let start = 0;
  let running = false;
  let pausedAt = 0;
  const duration = 8200;
  function grow(total) {
    while (visible < total) {
      const i = visible++;
      const point = points[i];
      if (i) {
        const parent = points[Math.floor((i - 1) / 2)];
        const bend = (parent.x + point.x) / 2;
        const path = document.createElementNS(ns, 'path');
        path.setAttribute('d', `M${parent.x},${parent.y} C${bend},${parent.y} ${bend},${point.y} ${point.x},${point.y}`);
        edges.append(path);
      }
      const circle = document.createElementNS(ns, 'circle');
      circle.setAttribute('cx', point.x);
      circle.setAttribute('cy', point.y);
      circle.setAttribute('r', i < 7 ? 4 : i < 31 ? 2.8 : 1.7);
      nodes.append(circle);
    }
  }
  function draw(progress) {
    const tripped = progress >= 1;
    const runaway = progress > 0.45;
    const danger = progress > 0.76;
    const total = Math.min(127, Math.floor(progress < 0.35 ? 1 + progress * 15 : 6 + 121 * ((progress - 0.35) / 0.65) ** 2.5));
    grow(total);
    count.textContent = String(total).padStart(3, '0');
    const burst = Math.min(15, Math.max(1, Math.round(1 + 14 * progress ** 2.4)));
    rate.replaceChildren(document.createTextNode(String(burst).padStart(2, '0')));
    const unit = document.createElement('span');
    unit.className = 'unit';
    unit.textContent = ' / 15';
    rate.append(unit);
    panel.classList.toggle('warning', runaway && !danger && !tripped);
    panel.classList.toggle('danger', danger && !tripped);
    panel.classList.toggle('tripped', tripped);
    phase.textContent = tripped ? 'CONTAINED' : danger ? 'RUNAWAY' : runaway ? 'ACCELERATING' : 'OBSERVING';
    sequence.textContent = tripped ? '03 CONTAIN' : runaway ? '02 DETECT' : '01 WATCH';
    elapsed.textContent = `T+${(progress * 8.2).toFixed(1).padStart(4, '0')}s`;
    let d = 'M0 30';
    for (let x = 0; x <= 180; x += 3) {
      const t = x / 180;
      const height = Math.max(1, (t * progress) ** 5 * 28);
      d += ` L${x} ${30 - height - Math.sin(x * 0.53) * progress * 1.2}`;
    }
    spark.setAttribute('d', d);
    if (tripped) {
      status.textContent = 'TRIPPED spawn_burst 15/10s > 15 · contained';
      button.textContent = '↻ Replay';
    }
  }
  function tick(now) {
    if (!running) return;
    if (!start) start = now;
    const progress = Math.min(1, (now - start) / duration);
    draw(progress);
    if (progress < 1) frame = requestAnimationFrame(tick);
    else running = false;
  }
  function replay() {
    cancelAnimationFrame(frame);
    nodes.replaceChildren();
    edges.replaceChildren();
    visible = 0;
    start = 0;
    pausedAt = 0;
    status.textContent = 'Watching spawn rate and tree shape.';
    button.textContent = '↻ Restart';
    if (motion.matches) {
      running = false;
      draw(1);
    } else {
      running = true;
      draw(0);
      frame = requestAnimationFrame(tick);
    }
  }
  button.addEventListener('click', replay);
  motion.addEventListener('change', replay);
  document.addEventListener('visibilitychange', () => {
    if (!running) return;
    if (document.hidden) {
      pausedAt = performance.now();
      cancelAnimationFrame(frame);
    } else {
      if (start && pausedAt) start += performance.now() - pausedAt;
      frame = requestAnimationFrame(tick);
    }
  });
  replay();
  const commands = {
    brew: 'brew install anujjjjjj/tap/fanoutfuse',
    go: 'go install github.com/anujjjjjj/fanoutfuse/cmd/fanoutfuse@latest',
    install: 'fanoutfuse install',
    run: 'fanoutfuse run'
  };
  document.querySelectorAll('[data-copy]').forEach(copyButton => {
    copyButton.addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(commands[copyButton.dataset.copy]);
        copyButton.textContent = 'Copied';
        document.querySelector('#copy-status').textContent = 'Command copied to clipboard.';
        setTimeout(() => { copyButton.textContent = 'Copy'; }, 1800);
      } catch {
        document.querySelector('#copy-status').textContent = 'Clipboard unavailable. Select the command to copy it.';
        copyButton.textContent = 'Select text';
      }
    });
  });
})();

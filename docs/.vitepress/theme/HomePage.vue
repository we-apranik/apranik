<template>
<div class="ap-home">

    <!-- 1. HERO -->
    <section class="hero"><div class="wrap">
      <h1><span>Censorship adapts.</span> <span>So we must.</span></h1>
      <p class="lead">Apranik is an open-source core that circumvention apps are built on. It's modular, so when censors block the usual ways through, a researcher can add a new method once and it reaches every app that uses it.</p>
      <!-- change A: one primary action, one quiet link, licence line -->
      <div class="btns" data-change="A"><a class="b p" :href="withBase('/get-started/install/')">Get started</a><a class="b" href="#loop">How it works</a></div>
      <p class="fine" data-change="A">Open source · MPL 2.0</p>
    </div></section>

    <!-- 2. PROBLEM -->
    <section class="alt prob"><div class="wrap">
      <h2>Blocking changes. Access shouldn't depend on it.</h2>
      <p>Censors don't block once. They watch what works, then block that. A method that connects people today can stop working without warning.</p>
      <p>Researchers keep finding new ways through. But a new method isn't something people can use yet. It has to be rebuilt, tested, and shipped inside each app, one at a time, and by the time it arrives, censors may have moved again.</p>
      <p>People lose access in the gap between a method being found and a method being usable.</p>
    </div></section>

    <!-- 3. THE LOOP -->
    <section id="loop"><div class="wrap">
      <h2>One loop, every time censors change.</h2>
      <div class="loopwrap">
        <div class="loopgrid">
          <div>
            <ol class="loop">
              <li class="st cen" data-step="0" data-t="Censors block a method" data-d="A method that was working stops working."><b>Censors block a method that was working</b><small>Censors</small></li>
              <li class="st ppl" data-step="1" data-t="A researcher finds a new way through" data-d="Someone finds a path that still gets through."><b>A researcher finds a new way through</b><small>Researchers</small></li>
              <li class="st ap" data-step="2" data-t="They add it to Apranik as a module" data-d="Only the new part is written. Everything else already exists in the core."><b>They add it to Apranik as a module</b><small>Researchers</small></li>
              <li class="st ap" data-step="3" data-t="It is reviewed and tested" data-d="The module is checked on its own and reviewed in the open."><b>It's reviewed and tested</b><small>Core maintainers</small></li>
              <li class="st ap" data-step="4" data-t="It ships in a release" data-d="It goes out once, in a release of the core."><b>It ships in a release</b><small>Core maintainers</small></li>
              <li class="st ppl" data-step="5" data-t="Clients and panels pick up the release" data-d="They update to the new release instead of rebuilding the method."><b>Clients and panels pick up the release</b><small>Client and panel maintainers</small></li>
              <li class="st ppl" data-step="6" data-t="People reconnect" data-d="People get back online through the apps they already use."><b>People reconnect</b><small>People using the apps</small></li>
            </ol>
            <div class="info loopinfo" aria-live="off"></div>
          </div>
          <svg class="loopsvg" viewBox="30 30 360 380" role="presentation" aria-hidden="true">
          <path class="lband" d="M356.2 243.4 A150 150 0 0 1 144.9 345.1"/>
          <path class="lseg" d="M242.7 63.6 A150 150 0 0 1 304.0 93.1"/>
          <polygon class="lhead" points="304.0,93.1 293.9,91.4 300.2,83.6"/>
          <path class="lseg" d="M344.9 144.3 A150 150 0 0 1 360.0 210.7"/>
          <polygon class="lhead" points="360.0,210.7 355.0,201.7 365.0,201.7"/>
          <path class="lseg ap" d="M345.4 274.5 A150 150 0 0 1 303.0 327.7"/>
          <polygon class="lhead ap" points="303.0,327.7 307.0,318.2 313.2,326.0"/>
          <path class="lseg ap" d="M244.0 356.1 A150 150 0 0 1 176.0 356.1"/>
          <polygon class="lhead ap" points="176.0,356.1 185.9,353.3 183.6,363.0"/>
          <path class="lseg" d="M117.0 327.7 A150 150 0 0 1 74.6 274.5"/>
          <polygon class="lhead" points="74.6,274.5 82.9,280.5 73.9,284.8"/>
          <path class="lseg" d="M60.0 210.7 A150 150 0 0 1 75.1 144.3"/>
          <polygon class="lhead" points="75.1,144.3 75.7,154.6 66.7,150.2"/>
          <path class="lseg" d="M116.0 93.1 A150 150 0 0 1 177.3 63.6"/>
          <polygon class="lhead" points="177.3,63.6 169.6,70.5 167.4,60.7"/>
          <g class="lnode cen" data-step="0"><circle cx="210.0" cy="60.0" r="19"/><text x="210.0" y="60.0" class="num" text-anchor="middle" dominant-baseline="central">1</text></g>
          <g class="lnode ppl" data-step="1"><circle cx="327.3" cy="116.5" r="19"/><text x="327.3" y="116.5" class="num" text-anchor="middle" dominant-baseline="central">2</text></g>
          <g class="lnode ap" data-step="2"><circle cx="356.2" cy="243.4" r="19"/><text x="356.2" y="243.4" class="num" text-anchor="middle" dominant-baseline="central">3</text></g>
          <g class="lnode ap" data-step="3"><circle cx="275.1" cy="345.1" r="19"/><text x="275.1" y="345.1" class="num" text-anchor="middle" dominant-baseline="central">4</text></g>
          <g class="lnode ap" data-step="4"><circle cx="144.9" cy="345.1" r="19"/><text x="144.9" y="345.1" class="num" text-anchor="middle" dominant-baseline="central">5</text></g>
          <g class="lnode ppl" data-step="5"><circle cx="63.8" cy="243.4" r="19"/><text x="63.8" y="243.4" class="num" text-anchor="middle" dominant-baseline="central">6</text></g>
          <g class="lnode ppl" data-step="6"><circle cx="92.7" cy="116.5" r="19"/><text x="92.7" y="116.5" class="num" text-anchor="middle" dominant-baseline="central">7</text></g>
          <text class="llabel" x="294.2" y="384.8" text-anchor="middle">Apranik</text>
          <text class="lcenter" x="210" y="199" text-anchor="middle">Censors adapt.</text><text class="lcenter" x="210" y="221" text-anchor="middle">So do we.</text>
          </svg>
        </div>
      </div>
      <p class="cap">Apranik covers the middle of the loop. A method is added, tested, and released once, so it doesn't have to be rebuilt inside every app.</p>
    </div></section>

    <!-- 4. BUILD ONCE -->
    <section class="alt"><div class="wrap">
      <h2>A new method shouldn't mean reinventing the wheel.</h2>
      <p class="sub">Without Apranik, a new method has to be rebuilt, tested and released inside every app, one at a time. That takes time, and by the time it is ready, censors may already have blocked it. With Apranik, it is added to the core once, and every app picks it up with a release.</p>
      <div class="ted" role="table" aria-label="Getting one new method to people, with and without Apranik">
        <div class="ted-head" role="row"><span role="columnheader">For each new method</span><span role="columnheader" class="bad"><b>Without Apranik</b><small>Every app on its own</small></span><span role="columnheader" class="ok"><b>With Apranik</b><small>One shared core</small></span></div>
        <div class="ted-row" role="row"><span class="q"><svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><circle cx="9" cy="8" r="3"/><circle cx="17" cy="9" r="2.4"/><path d="M3 19c0-3.3 2.7-5 6-5s6 1.7 6 5M15 14.5c3 0 6 1 6 4.5"/></svg><span>Who does the work, and how often?</span></span><span class="a bad"><i aria-hidden="true">✕</i><b>Every app’s team, separately, for every new method</b><em>The same work is repeated by many teams, and it grows with every app and every method.</em></span><span class="a ok"><i aria-hidden="true">✓</i><b>One team, once, in the core</b><em>The work is done and reviewed in one place.</em></span></div>
        <div class="ted-row" role="row"><span class="q"><svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="8"/><path d="M12 7v5l3 2"/></svg><span>When can people use a new method?</span></span><span class="a bad"><i aria-hidden="true">✕</i><b>Only after each app has rebuilt, tested and released it</b><em>People wait even though the method already exists.</em></span><span class="a ok"><i aria-hidden="true">✓</i><b>After one core release, when apps update</b><em>People get it as soon as their app updates.</em></span></div>
        <div class="ted-row" role="row"><span class="q"><svg class="ico" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3l7 3v5c0 5-3 8-7 10-4-2-7-5-7-10V6z"/></svg><span>What if censors block it first?</span></span><span class="a bad"><i aria-hidden="true">✕</i><b>Many apps are too late</b><em>A slow rollout gives censors time to block it before it reaches many people.</em></span><span class="a ok"><i aria-hidden="true">✓</i><b>Apps already have it, or get the fix together</b><em>A shorter wait leaves censors less time to react.</em></span></div></div>
    </div></section>

    <!-- 5. HOW IT WORKS -->
    <section class="how"><div class="wrap">
      <h2>Build it once. Everyone gets it.</h2>
      <p>Apranik is the core that clients and server panels build on. It handles the connection itself, so they don't each have to.</p>
      <p>It's modular. Each part of a connection is its own module, and a new method is another module that plugs into the same place. A researcher can add one without changing the rest, and a maintainer can review it on its own.</p>
      <svg class="metro" viewBox="0 0 760 300" role="img" aria-label="A researcher writes a module, the core maintainers review it, and it ships in a core release. From there four lines carry it to two clients and two panels, which only update the core.">
          <g class="lines">
            <path class="l-blue" d="M470 150L570 50H650"/>
            <path class="l-blue" d="M490 150L530 110H650"/>
            <path class="l-blue" d="M490 150L530 190H650"/>
            <path class="l-blue" d="M470 150L570 250H650"/>
            <path class="l-green" d="M60 150H490"/>
          </g>
          <g class="stops">
            <circle cx="60" cy="150" r="9"/><circle cx="230" cy="150" r="9"/><circle cx="400" cy="150" r="9"/>
            <circle cx="650" cy="50" r="9"/><circle cx="650" cy="110" r="9"/><circle cx="650" cy="190" r="9"/><circle cx="650" cy="250" r="9"/>
          </g>
          <g class="names">
            <text x="60" y="118" text-anchor="middle">Researcher</text>
            <text x="230" y="118" text-anchor="middle">Core maintainers</text>
            <text x="400" y="118" text-anchor="middle">Core release</text>
            <text x="672" y="56">Client</text><text x="672" y="116">Client</text><text x="672" y="196">Panel</text><text x="672" y="256">Panel</text>
          </g>
          <text class="sub" x="60" y="186" text-anchor="middle">writes a module</text>
          <text class="sub" x="230" y="186" text-anchor="middle">reviews it on its own</text>
          <text class="sub" x="400" y="186" text-anchor="middle">ships to everyone</text>
          <text class="stage" x="60" y="86" text-anchor="middle">ADD</text>
          <text class="stage" x="230" y="86" text-anchor="middle">REVIEW</text>
          <text class="stage" x="400" y="86" text-anchor="middle">SHIP</text>
          <text class="stage" x="650" y="20" text-anchor="middle">UPDATE</text>
        </svg>
        <p class="metro-note">Each app updates the core. Nothing is rebuilt.</p>
        <ol class="mlist">
          <li><b>Researcher</b> writes a module.</li>
          <li><b>Core maintainers</b> review it on its own.</li>
          <li><b>Core release</b> ships it to everyone.</li>
          <li><b>Every client and panel</b> updates the core. Nothing is rebuilt.</li>
        </ol>
    </div></section>

    <!-- 7. PRINCIPLES -->
    <section class="ob"><div class="wrap">
      <h2>Built in the open, by the people who need it.</h2>
      <div class="cards">
        <div class="card"><div class="ic"><svg viewBox="0 0 24 24"><path d="M8 7 3 12l5 5M16 7l5 5-5 5"/></svg></div><h3>Open source</h3><p>The code is public. Anyone can read it, check it, and carry it forward.</p></div>
        <div class="card"><div class="ic"><svg viewBox="0 0 24 24"><path d="M12 3v4M12 17v4M3 12h4M17 12h4"/><circle cx="12" cy="12" r="3"/></svg></div><h3>Free for everyone</h3><p>Free to use, and free to adapt in any community.</p></div>
        <div class="card"><div class="ic"><svg viewBox="0 0 24 24"><circle cx="9" cy="8" r="3"/><circle cx="17" cy="9" r="2.4"/><path d="M3 19c0-3.3 2.7-5 6-5s6 1.7 6 5M15 14.5c3 0 6 1 6 4.5"/></svg></div><h3>Community-built</h3><p>Researchers and maintainers shape it together.</p>
          <div class="crew" data-repo="we-apranik/apranik"><span class="faces"></span><span class="more" hidden></span><a class="seat" :href="withBase('/contribute/')" title="Room for one more" aria-label="Room for one more: read how to contribute">+</a></div></div>
      </div>
    </div></section>

    <!-- 8. DOORS -->
    <section class="alt"><div class="wrap">
      <h2>Everyone has a part.</h2>
      <div class="roles">
        <div>
          <h3>Build with Apranik</h3>
          <a :href="withBase('/contribute/')"><span><b>Researcher</b>Found a new way through? Add it as a module and let clients and panels pick it up.</span><i aria-hidden="true">→</i></a>
          <a :href="withBase('/contribute/')"><span><b>Core contributor</b>Help build and review the core.</span><i aria-hidden="true">→</i></a>
        </div>
        <div>
          <h3>Build on Apranik</h3>
          <a :href="withBase('/configuration/')"><span><b>Client developer</b>Build your client on the core. New methods arrive with each release.</span><i aria-hidden="true">→</i></a>
          <a :href="withBase('/configuration/')"><span><b>Panel developer</b>Build your server panel on the core, with the same benefit.</span><i aria-hidden="true">→</i></a>
        </div>
      </div>
    </div></section>

    <!-- 8b. WHAT'S INCLUDED -->
    <section><div class="wrap">
      <h2>Bring the new part. The rest is here.</h2>
      <p class="sub">Everything below is already built into the core.</p>
      <div class="incl"><b>Transports</b><span class="chip">TCP</span><span class="chip">UDP</span><span class="chip">TLS</span><span class="chip">WebSocket</span><span class="chip">HTTPUpgrade</span><span class="chip">PROXY protocol</span><span class="chip">Fragment</span></div>
      <div class="incl"><b>Protocols</b><span class="chip">SOCKS5</span><span class="chip">HTTP</span><span class="chip">Trojan</span><span class="chip">VLESS</span></div>
      <div class="incl"><b>Routing</b><span class="chip">Rules</span><span class="chip">Datasets</span><span class="chip">Block</span><span class="chip">Route</span></div>
      <div class="incl"><b>DNS</b><span class="chip">UDP</span><span class="chip">TCP</span><span class="chip">DoT</span><span class="chip">DoH</span><span class="chip">Cache</span></div>
      <div class="incl"><b>Sniffers</b><span class="chip">TLS</span><span class="chip">HTTP</span></div>
      <a class="lnk" :href="withBase('/configuration/')">See everything that's supported</a>
    </div></section>

    <!-- 9. SECURITY AND TRUST -->
    <section><div class="wrap">
      <h2>Don't trust. Check.</h2>
      <div class="trio">
        <div><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/></svg><h3>The code is public</h3><p>Read it, run it, question it.</p><a :href="repositoryUrl + ''">Read the code</a></div>
        <div><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="M8 12.5l3 3 5-6"/></svg><h3>Every change is reviewed</h3><p>Nothing ships without a second pair of eyes.</p><a :href="repositoryUrl + '/pulls'">See the changes</a></div>
        <div><svg viewBox="0 0 24 24" aria-hidden="true"><rect x="3" y="5" width="18" height="14" rx="2"/><path d="M3 7l9 6 9-6"/></svg><h3>Reports are private</h3><p>They go to the maintainers by email, and a fix ships before the details are public.</p><a :href="withBase('/get-started/security/')">See how reports are handled</a></div>
      </div>
    </div></section>

    <!-- 10. JOIN -->
    <section class="join"><div class="wrap">
      <h2>Join the loop.</h2>
      <p class="sub">Apranik is built in the open. Read the code, talk with the people building it, or send a first change.</p>
      <!-- TODO: add the real Telegram URL, then restore the Telegram button -->
      <div class="btns"><a class="b p" :href="repositoryUrl + ''">GitHub</a></div>
    </div></section>

    <!-- 10b. FAQ (10 questions max) -->
    <section id="faq"><div class="wrap">
      <h2>Straight answers.</h2>
      <div class="faq">
        <details><summary>What is Apranik?</summary><p>An open-source core that circumvention apps and server panels are built on. It handles the connection itself, so each app doesn't have to. New ways through are added to it as separate modules. It is not another VPN app: you don't install Apranik and use it directly, it's the layer underneath the apps and panels that people use.</p></details>
        <details><summary>Who is it for?</summary><p>Researchers who find new methods, developers who build clients and server panels, and maintainers of the core. You don't need to be a developer to use an app built on Apranik.</p></details>
        <details><summary>What does the name mean?</summary><p>Apranik was a Sasanian commander who fought the Arab invasion of Persia (Iran) in the seventh century. By tradition, she kept up a guerrilla war after the empire fell, and her motto was “No retreat, no surrender.”</p></details>
        <details><summary>How is it different from other proxy tools?</summary><p>It's built around adding new methods as separate modules, so a researcher can contribute one without learning the whole project, and a maintainer can review it on its own.</p></details>
        <details><summary>Is it free, and who can use it?</summary><p>It's free to use and free to adapt, and any community can adapt it to the censorship it faces, wherever it is. It is released under the <a :href="repositoryUrl + '/blob/main/LICENSE'">Mozilla Public License 2.0</a>.</p></details>
        <details><summary>How do I add a new method?</summary><p>Write it as a module, test it on its own, and send it for review. Every change is reviewed before it ships. The <a :href="withBase('/contribute/')">contributing guide</a> walks through it.</p></details>
        <details><summary>Is it safe to use?</summary><p>Apranik is open source, so anyone can read and check the code. Every change is reviewed before it ships, and vulnerability reports are handled privately. The <a :href="withBase('/get-started/security/')">security page</a> says how to report one.</p></details>
        <details><summary>Does it make me anonymous?</summary><p>Apranik's job is to get you around blocking. Anonymity is a different goal that needs its own tools, so if it matters to you, plan for it separately.</p></details>
        <details><summary>Will it always work?</summary><p>Apranik is built for a moving target. Censors keep blocking what works, so no single method lasts forever. That is why new methods are added as modules and shipped to every app in one release: access can adapt as fast as blocking does.</p></details>
        <details><summary>How do I report a security issue?</summary><p>Email we-apranik@proton.me, and please don't post it publicly. The <a :href="withBase('/get-started/security/')">security page</a> says what to include.</p></details>
      </div>
    </div></section>

    <!-- 11. SITE FOOTER -->
    <footer class="foot"><div class="wrap">
      <div class="cols">
        <div class="brand"><b>Apranik</b><p>An open-source core that circumvention apps are built on.</p></div>
        <div><h3>Learn</h3><a :href="withBase('/get-started/overview/')">Overview</a><a :href="withBase('/concepts/architecture/')">Architecture</a><a :href="withBase('/configuration/')">Configuration</a><a :href="withBase('/get-started/faq/')">FAQ</a></div>
        <div><h3>Contribute</h3><a :href="withBase('/contribute/')">Contributing guide</a><a :href="withBase('/contribute/good-first-issues/')">Good first issues</a><a :href="withBase('/get-started/security/')">Report a vulnerability</a></div>
        <div><h3>Trust</h3><a :href="withBase('/get-started/security/')">Security</a><a :href="repositoryUrl + ''">Source code</a><a :href="repositoryUrl + '/blob/main/LICENSE'">License (MPL 2.0)</a>
        </div>
      </div>
      <p class="legal">Apranik is open source. Licensed under MPL 2.0.</p>
    </div></footer>

    <button class="totop" type="button" aria-label="Back to top" hidden>
      <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 19V5M5 12l7-7 7 7"/></svg>
    </button>

  </div>
</template>

<script setup>
import { onBeforeUnmount, onMounted } from "vue";
import { withBase } from "vitepress";

const repositoryUrl = "https://github.com/we-apranik/apranik";


function initBackToTop() {
  var button = document.querySelector(".ap-home .totop");
  if (!button) return;
  var hero = document.querySelector(".ap-home .hero");
  if (hero && "IntersectionObserver" in window) {
    // Shown once the hero has scrolled away; no work on every scroll event.
    new IntersectionObserver(function (entries) {
      button.hidden = entries[0].isIntersecting;
    }).observe(hero);
  } else {
    button.hidden = false;
  }
  button.addEventListener("click", function () {
    var calm = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    window.scrollTo({ top: 0, behavior: calm ? "auto" : "smooth" });
    document.querySelector(".VPNavBar a").focus({ preventScroll: true });
  });
}

var contributorsRequested = false;
var contributorsShown = 3;
var contributorsFetched = 30;

function showContributors(section) {
  var crew = section.querySelector(".crew");
  if (contributorsRequested || !crew) return;
  contributorsRequested = true;
  var url = "https://api.github.com/repos/" + crew.dataset.repo + "/contributors?per_page=" + contributorsFetched;
  fetch(url, {
    headers: { Accept: "application/vnd.github+json" },
    referrerPolicy: "no-referrer",
    credentials: "omit"
  }).then(function (response) {
    return response.ok ? response.json() : [];
  }).then(function (contributors) {
    var people = contributors.filter(function (c) { return c.type === "User"; });
    if (!people.length) return;
    var faces = crew.querySelector(".faces");
    people.slice(0, contributorsShown).forEach(function (person) {
      var photo = document.createElement("img");
      photo.src = person.avatar_url + (person.avatar_url.indexOf("?") < 0 ? "?" : "&") + "s=56";
      photo.alt = person.login + ", contributor";
      photo.title = person.login;
      photo.width = 28;
      photo.height = 28;
      photo.loading = "lazy";
      photo.referrerPolicy = "no-referrer";
      faces.appendChild(photo);
    });
    var others = people.length - contributorsShown;
    if (others > 0) {
      var more = crew.querySelector(".more");
      // The list is cut at contributorsFetched, so a full page may hide more people than it counts.
      more.textContent = contributors.length >= contributorsFetched ? "and more" : "+" + others + " more";
      more.hidden = false;
    }
  }).catch(function () { /* offline or blocked: the empty seat alone is fine */ });
}

function initContributors() {
  var section = document.querySelector(".ap-home .ob");
  if (!section) return;
  contributorsRequested = false;
  if ("IntersectionObserver" in window) {
    // Nothing is requested from GitHub unless the visitor scrolls here.
    var watcher = new IntersectionObserver(function (entries) {
      if (entries[0].isIntersecting) { watcher.disconnect(); showContributors(section); }
    }, { rootMargin: "200px" });
    watcher.observe(section);
  } else {
    showContributors(section);
  }
}

var loopTimer = null;
var keyHandler = null;

function init() {
  initBackToTop();
  initContributors();
  if (loopTimer) { clearInterval(loopTimer); loopTimer = null; }
  var wrap = document.querySelector(".ap-home .loopwrap");
  var steps = document.querySelectorAll(".ap-home [data-step]");
  var infoBoxes = document.querySelectorAll(".ap-home .loopinfo");
  if (!wrap || !steps.length) return;
  var stepCount = document.querySelectorAll(".ap-home .loop li.st").length;
  var current = 0;

  function pick(index) {
    current = index;
    var source = null;
    steps.forEach(function (x) {
      var on = x.dataset.step === String(index);
      x.classList.toggle("sel", on);
      if (on && !source) source = x;
    });
    var accent = source.classList.contains("ap") ? "var(--ap-green)"
      : source.classList.contains("ppl") ? "var(--ap-blue)" : "var(--ap-red)";
    infoBoxes.forEach(function (box) {
      box.style.borderLeftColor = accent;
      box.innerHTML = "<b>" + source.dataset.t + "</b><span>" + source.dataset.d + "</span>";
    });
  }

  pick(0);

  var reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  var visible = false;

  function startTimer() {
    if (loopTimer) clearInterval(loopTimer);
    loopTimer = null;
    if (reduceMotion) return;
    loopTimer = setInterval(function () {
      if (visible) pick((current + 1) % stepCount);
    }, 3000);
  }

  function choose(index) {
    pick(index);
    startTimer();
  }

  steps.forEach(function (x) {
    if (x.tagName === "LI") {
      x.tabIndex = 0;
      x.setAttribute("role", "button");
    }
    x.addEventListener("click", function () { choose(Number(x.dataset.step)); });
    x.addEventListener("keydown", function (e) {
      if (e.key === "Enter" || e.key === " ") { e.preventDefault(); choose(Number(x.dataset.step)); }
    });
  });

  new IntersectionObserver(function (entries) {
    visible = entries[0].isIntersecting;
  }).observe(wrap);

  // Left and right arrows move between the steps while the loop is on screen.
  if (keyHandler) document.removeEventListener("keydown", keyHandler);
  keyHandler = function (e) {
    if (!visible || e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return;
    if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    var target = e.target;
    if (target && (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName))) return;
    e.preventDefault();
    choose((current + (e.key === "ArrowRight" ? 1 : stepCount - 1)) % stepCount);
  };
  document.addEventListener("keydown", keyHandler);

  if (reduceMotion) {
    pick(2);
    return;
  }
  startTimer();
}

onMounted(init);
onBeforeUnmount(function () {
  if (loopTimer) { clearInterval(loopTimer); loopTimer = null; }
  if (keyHandler) { document.removeEventListener("keydown", keyHandler); keyHandler = null; }
});
</script>

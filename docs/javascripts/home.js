(function () {
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
      document.querySelector(".md-header a, .md-header button").focus({ preventScroll: true });
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
      document.querySelectorAll(".ap-home .lseg").forEach(function (segment, segmentIndex) {
        segment.classList.toggle("flow", segmentIndex === (index + 6) % 7);
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

    if (reduceMotion) {
      pick(2);
      return;
    }

    new IntersectionObserver(function (entries) {
      visible = entries[0].isIntersecting;
      // Infinite animations off screen still repaint every frame while scrolling.
      wrap.classList.toggle("offscreen", !visible);
    }).observe(wrap);
    wrap.classList.add("offscreen");
    startTimer();
  }

  if (typeof document$ !== "undefined") {
    document$.subscribe(init);
  } else if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();

# FAQ

## What is Apranik?

An open-source core that circumvention apps and server panels are built on. It handles the connection itself, so each app doesn't have to. New ways through are added to it as separate modules. It is not another VPN app: you don't install Apranik and use it directly, it's the layer underneath the apps and panels that people use.

## Who is it for?

Researchers who find new methods, developers who build clients and server panels, and maintainers of the core. You don't need to be a developer to use an app built on Apranik.

## What does the name mean?

Apranik was a commander of the late Sasanian army who, by tradition, kept fighting a guerrilla war after the empire fell in the seventh century.

## How is it different from other proxy tools?

It's built around adding new methods as separate modules, so a researcher can contribute one without learning the whole project, and a maintainer can review it on its own.

## Is it free, and is it only for one country?

It's free to use and free to adapt, and any community can adapt it to the censorship it faces. **[TODO: confirm the license before publishing.]**

## How do I add a new method?

Write it as a module, test it on its own, and send it for review. Every change is reviewed before it ships. The [contributing guide](contributing.md) walks through it.

## Is it safe to use?

We don't promise that. The code is public so you can check it, and vulnerability reports are handled privately. The [threat model](design/threat-model.md) says what we do and don't claim.

## Does it make me anonymous?

No. Getting around blocking and staying anonymous are different things, and Apranik is built for the first. If anonymity matters to you, treat it as a separate question.

## Will it always work?

No. No circumvention method works forever, because censors watch what works and block it. That's why Apranik is built so new methods can be added quickly.

## How do I report a security issue?

Use the private channel on the security page, and please don't post it publicly. **[TODO: add the link to the private reporting channel.]**


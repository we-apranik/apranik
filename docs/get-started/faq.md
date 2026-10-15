# FAQ

## What is Apranik?

An open-source core that circumvention apps and server panels are built on. It handles the connection itself, so each app doesn't have to. New ways through are added to it as separate modules. It is not another VPN app: you don't install Apranik and use it directly, it's the layer underneath the apps and panels that people use.

## Who is it for?

Researchers who find new methods, developers who build clients and server panels, and maintainers of the core. You don't need to be a developer to use an app built on Apranik.

## What does the name mean?

Apranik was a Sasanian commander who fought the Arab invasion of Persia (Iran) in the seventh century. By tradition, she kept up a guerrilla war after the empire fell, and her motto was “No retreat, no surrender.”

## How is it different from other proxy tools?

It's built around adding new methods as separate modules, so a researcher can contribute one without learning the whole project, and a maintainer can review it on its own.

## Is it free, and who can use it?

It's free to use and free to adapt, and any community can adapt it to the censorship it faces, wherever it is. It is released under the [Mozilla Public License 2.0](https://github.com/we-apranik/apranik/blob/main/LICENSE).

## How do I add a new method?

Write it as a module, test it on its own, and send it for review. Every change is reviewed before it ships. The [contributing guide](../contribute/index.md) walks through it.

## Is it safe to use?

Apranik is open source, so anyone can read and check the code. Every change is reviewed before it ships, and vulnerability reports are handled privately. The [security page](security.md) says how to report one.

## Does it make me anonymous?

Apranik's job is to get you around blocking. Anonymity is a different goal that needs its own tools, so if it matters to you, plan for it separately.

## Will it always work?

Apranik is built for a moving target. Censors keep blocking what works, so no single method lasts forever. That is why new methods are added as modules and shipped to every app in one release: access can adapt as fast as blocking does.

## How do I report a security issue?

Report it privately, and please don't post it publicly. The [security page](security.md) has the GitHub reporting link and an email address.


---
title: "0. Overview"
description: "Orient before you build: see what the agent is grounded in, decide when an agent is justified, learn what a claim in this course is worth, and pick your path."
slug: "0-overview"
---

{{% admonition abstract "In one glance" %}}

- **You will:** Decide whether an agent is the right tool for a job, learn what this course's green checkmarks are worth, and pick your model path.
- **You need:** Nothing installed. Chapter 0 is read-only.
- **Time:** about 4 minutes, orientation. {{% /admonition %}}

## Three incidents are open and one of them is a SEV1

The world this course works in is small, fictional, and already committed to the repository. Before you read the rows below, guess how many of them a language model could plausibly have memorised during training:

```bash
sqlite3 -header -column agents/data/incidents.db \
  "select id, service, severity, status from incidents where status = 'open';"
```

```text
  id       service    severity  status
-------  -----------  --------  ------
INC-002  inventory    SEV1      open
INC-005  search       SEV3      open
INC-010  api-gateway  SEV3      open
```

None of them. That file is the only source for `INC-002` — ten incidents, four service logs, seven runbooks, two Agent Skills — so every claim the agent makes about it has to come from there or from nowhere. Inventory is down, and somebody has to decide whether restarting it clears the fault or destroys the record of what broke. You will hand that decision to an agent, then spend the rest of the course working out whether the agent deserved it.

You cannot run that query yet, and that is deliberate: `sqlite3` arrives with the pinned toolchain in [1.0. System]({{< relref "/1. Setup/1.0. System.md" >}}). Read it for now as the transcript it is.

## Three pages now, six for later

Chapter 0 is the only part of the course you cannot run, so it earns its place by being short and by ending in decisions rather than exercises; the hands-on work starts the moment you install. Read these three in order:

- **[0.0. Course]({{< relref "/0. Overview/0.0. Course.md" >}})** _(orientation · ~8 min)_: what you will have built, which path to take, and what it costs.
- **[0.1. Agents]({{< relref "/0. Overview/0.1. Agents.md" >}})** _(concept · ~12 min)_: what an agent actually is, and when a plain function beats one.
- **[0.2. Evidence]({{< relref "/0. Overview/0.2. Evidence.md" >}})** _(concept · ~8 min)_: what a gate proves, what an observation suggests, and why the course never blurs them.

The other six are reference — open them when a chapter sends you. [0.3. AgentOps]({{< relref "/0. Overview/0.3. AgentOps.md" >}}) maps the lifecycle to the chapters, [0.4. Ecosystem]({{< relref "/0. Overview/0.4. Ecosystem.md" >}}) says who owns which boundary and port, [0.5. Provider Options]({{< relref "/0. Overview/0.5. Provider Options.md" >}}) settles the model choice, [0.6. Resources]({{< relref "/0. Overview/0.6. Resources.md" >}}) says which source wins when two disagree, [0.7. Troubleshooting]({{< relref "/0. Overview/0.7. Troubleshooting.md" >}}) fixes a failing checkpoint, and [0.8. Glossary]({{< relref "/0. Overview/0.8. Glossary.md" >}}) defines every term in one line — keep that one open in a second tab.

## What this chapter proved

- The agent's world is a committed SQLite seed with ten incidents in it, three of them open and one a SEV1.
- You know which three pages to read now and which six to bookmark.
- You know that Chapter 0 installs nothing: [1.0. System]({{< relref "/1. Setup/1.0. System.md" >}}) owns the clone and the toolchain.

You now know what this repository contains, what the course will make you responsible for, and which page to open first. That last one is the only decision this page was ever asking for.

Continue to [0.0. Course]({{< relref "/0. Overview/0.0. Course.md" >}}), which shows you the first thing the agent does with `INC-002`.

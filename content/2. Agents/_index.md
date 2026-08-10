---
title: "2. Agents"
description: Run and understand the completed Google ADK 2.x reference agent end to end on local Qwen3.
slug: "2-agents"
---

{{% admonition abstract "In one glance" %}}

- **You will:** See how the whole chapter fits together, then prove the agent assembles correctly without starting a model.
- **You need:** Chapter 1 finished, with `mise run doctor` and `mise run doctor:model` passing.
- **Time:** about 8 minutes, orientation. {{% /admonition %}}

## What will you understand in this chapter?

This chapter builds one object, the root agent `Compose.RootAgent()` returns. Every later chapter adds to that same object rather than replacing it.

That object is the **AgentOps Agent**, the single reference agent carried through the entire course. It is assembled once in `compose/composition.go` as a plain ADK `llmagent.Config`: a model, an instruction string, and a flat tool list, plus a set of **policy callbacks** attached once at the application level — code the runtime runs around every model call and tool call.

{{% collapsible note "Deeper: where does this agent go after Chapter 2?" %}}

Every later chapter instruments _this same object_: Chapter 3 hangs capabilities off it, Chapter 4 wraps it in quality gates, Chapters 5 and 6 put it behind a gateway and onto Kubernetes, and Chapter 7 observes it in production.

[Chapter 3]({{< relref "/3. Capabilities/_index.md" >}}) deepens its tools, knowledge, workflows, and delegation; [Chapter 8.7]({{< relref "/8. Community/8.7. Capstone.md" >}}) asks you to adapt these boundaries to your own domain. {{% /collapsible %}}

Read the sections by their kind, not just their order. **2.0 is conceptual**: the mental model you need before code makes sense. **2.1 and 2.5 are hands-on**: you run commands and see output. **2.2, 2.3, and 2.4 are reference**: the model, instruction, and runtime pieces you consult as you build.

- **[2.0. Concepts]({{< relref "/2. Agents/2.0. Concepts.md" >}})** _(concept)_: The ADK 2.x building blocks — Agent, Runner, Session, Events, Tools, and the graph Workflow.
- **[2.1. First Agent]({{< relref "/2. Agents/2.1. First Agent.md" >}})** _(hands-on)_: Inspect and run the AgentOps Agent end to end on local Qwen3.
- **[2.2. Models]({{< relref "/2. Agents/2.2. Models.md" >}})** _(reference)_: The default Ollama contract and the optional native Gemini branch.
- **[2.3. Instructions]({{< relref "/2. Agents/2.3. Instructions.md" >}})** _(hands-on)_: The system instruction, its enforcement map, and a deterministic red/green trajectory contract.
- **[2.4. Sessions]({{< relref "/2. Agents/2.4. Sessions.md" >}})** _(hands-on)_: Persistent ADK sessions, **A2A** tasks (units of work exchanged between agents across process boundaries), lifecycle ownership, and resettable runtime state.
- **[2.5. Dev Loop]({{< relref "/2. Agents/2.5. Dev Loop.md" >}})** _(hands-on)_: Offline gates, interactive modes, model-backed evaluations, and failure diagnosis.

By the end you will have run the agent on a model on your own laptop. You will know which file picks the model, which string sets its behavior, where a conversation is stored, and which command proves it all works without a model.

## Which page owns which part of the agent?

The `llmagent.Config` that `Compose.conversationalConfig()` assembles in `compose/composition.go` names each part of the reference agent. Each part is taught by exactly one sub-page, so when a behavior surprises you, there is one page and one module to open.

Concretely, each field of the root agent `Compose.RootAgent()` returns traces to one owner:

| Sub-page                                                              | What it teaches                                            | Owning module / symbol                                                                                                                     |
| --------------------------------------------------------------------- | ---------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| [2.0. Concepts]({{< relref "/2. Agents/2.0. Concepts.md" >}})         | The ADK runtime loop and its object vocabulary             | `google.golang.org/adk/v2` (framework)                                                                                                     |
| [2.1. First Agent]({{< relref "/2. Agents/2.1. First Agent.md" >}})   | Composing and running `Compose.RootAgent()`                | `compose/composition.go` (composition root)                                                                                                |
| [2.2. Models]({{< relref "/2. Agents/2.2. Models.md" >}})             | Provider selection behind `llmagent.Config.Model`          | `model/model.go` `model.Build`, `config/config.go` `ModelProvider`                                                                         |
| [2.3. Instructions]({{< relref "/2. Agents/2.3. Instructions.md" >}}) | The persona and rules behind `llmagent.Config.Instruction` | `compose/composition.go` `compose.Instruction()`                                                                                           |
| [2.4. Sessions]({{< relref "/2. Agents/2.4. Sessions.md" >}})         | Persistent sessions and A2A task state                     | `a2aserver/a2aserver.go` `Config.SessionService`, `cmd/agent/session_store.go` `database.NewSessionService`, `config/config.go` `StateDir` |
| [2.5. Dev Loop]({{< relref "/2. Agents/2.5. Dev Loop.md" >}})         | The offline gates and interactive run modes                | `mise.toml` tasks                                                                                                                          |

Tools and policy hooks are named here, not taught here. Owned by [Chapter 3]({{< relref "/3. Capabilities/_index.md" >}}) and [4.5. Guardrails]({{< relref "/4. Quality/4.5. Guardrails.md" >}}).

{{% collapsible note "Deeper: the same map as a diagram, and who owns tools and policy" %}}

This diagram maps the anatomy to its owners:

```mermaid
flowchart TD
    concepts["Runtime concepts · 2.0<br/>Agent · Runner · Session · Events"]
    subgraph agent["Compose.RootAgent() — assembled in compose/composition.go · 2.1"]
        model["Model = model.Build() · 2.2"]
        instr["Instruction = compose.Instruction() · 2.3"]
        tools["Tools = reads · actions · memory<br/>Toolsets = skills<br/>policy plugin on App · Ch. 3 / 4.5"]
    end
    runtime["Persistent runtime · 2.4<br/>Config.SessionService · A2A tasks · a2aserver/a2aserver.go"]
    loop["Dev loop · 2.5<br/>mise run test · run · web · a2a"]
    concepts --> agent
    agent --> runtime
    loop -. iterates .-> agent
```

**Diagram in words:** Runtime concepts lead to one agent assembled from a model, instruction, tools, and an app-level policy plugin; persistent runtime and the dev loop surround it.

The `llmagent.Config.Tools` and `.Toolsets` lists and the app plugin belong to later chapters: 2.1 shows the wiring, [Chapter 3]({{< relref "/3. Capabilities/_index.md" >}}) owns each tool, and [4.5. Guardrails]({{< relref "/4. Quality/4.5. Guardrails.md" >}}) owns policy. This page only names the seams. {{% /collapsible %}}

## What proves this chapter worked?

Two things prove the chapter. The first is one command, and it never starts a model:

```bash
cd agents/go
mise run test
```

That is the offline test suite. The required drill in [2.3. Instructions]({{< relref "/2. Agents/2.3. Instructions.md#your-turn-which-case-catches-a-deleted-rule" >}}) is deterministic too: delete one instruction rule, watch the focused contract fail, then restore it. A live-model comparison remains optional evidence.

The whole run can take several minutes depending on the machine and cache state. It ends with the Go test result and one coverage line per package, and it fails when any package falls under the 80% line-coverage floor, which catches code that shipped without tests. Nothing in it needs a model or a network, so a red line is a real failure rather than a missing piece of setup.

A green run proves the agent is assembled correctly, not that it reasons well. Model-backed evaluation is a separate evidence lane, owned by [2.5. Dev Loop]({{< relref "/2. Agents/2.5. Dev Loop.md" >}}).

{{% collapsible note "Deeper: can you test the model and config wiring on its own?" %}}

That is the umbrella gate (`go test` over the full suite). To verify just this chapter's seams in isolation, run the model and config tests directly:

```bash
cd agents/go
go test ./model ./config
```

That focused subset exits cleanly and gives fast feedback. `mise run test` adds race detection and a coverage profile around the complete suite, then rejects any package under 80% — a floor on how much code the tests reach, not on how good they are.

Those cover provider resolution and the fail-fast cross-field checks in `config/config.go` — a bad `AGENT_MODEL_PROVIDER` combination fails at construction with a message that names the fix, not deep inside a turn. Model-backed behavior stays a separate evidence path ([2.5. Dev Loop]({{< relref "/2. Agents/2.5. Dev Loop.md" >}})'s `mise run eval`), because a green offline suite proves the agent is assembled correctly, not that it reasons well. {{% /collapsible %}}

**You are done when:**

- `mise run test` finishes in `agents/go` with no failures and reports the measured coverage.
- You finished the required drill in [2.3. Instructions]({{< relref "/2. Agents/2.3. Instructions.md#your-turn-which-case-catches-a-deleted-rule" >}}): the focused offline contract went red without the direct skill-load rule, green after the scoped restore, and the live-model comparison remained optional evidence.
- You can name the one object every later chapter adds to, and the file that assembles it.
- You can say which sub-page owns the model, which owns the instruction, which owns the session store, and which owns the dev loop.
- You know which two pages ask you to run something and which three you will come back to as reference.
- Without reopening Chapter 1: you can name the command that proves the environment offline and the directory you run it from, and say why a passing `mise run test` reads no `.env`.

Continue to [2.0. Concepts]({{< relref "/2. Agents/2.0. Concepts.md" >}}) when `mise run test` passes on your machine, because every page after this one assumes the agent already builds.

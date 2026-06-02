# Changelog

All notable changes to this project are documented here.

## [0.3.4] - 2026-06-02


### Added

- Add Windows support to bash tool ([c92ef7c](https://github.com/feimingxliu/ub/commit/c92ef7c9eb7296459b8a3757eb2e7f17ad9553e7))


### Fixed

- Improve TUI resume history handling ([22538bd](https://github.com/feimingxliu/ub/commit/22538bdd33bac495d1abfe08a800e22d122a89bc))
- Polish TUI activity interactions ([dd0f778](https://github.com/feimingxliu/ub/commit/dd0f778eb375e6d640b6a7a809ea8a504e223208))
- Add TUI transcript jump shortcuts ([445ba08](https://github.com/feimingxliu/ub/commit/445ba08a2d38e0bfdf2b4e9bd38a98ac5b57c366))
- Stabilize tui session utilities ([6e2ac55](https://github.com/feimingxliu/ub/commit/6e2ac551c0472994e274f1df3f4ecc14f45a20dc))
- Remove default tool loop cap ([07d6322](https://github.com/feimingxliu/ub/commit/07d632254552eea0340a7536d9996c40d1576913))
- Improve TUI tool detail display ([66bf1a8](https://github.com/feimingxliu/ub/commit/66bf1a8827c227363ee90bdddb3bb466b06caf0c))
- Stabilize Windows tests ([4e9b8a5](https://github.com/feimingxliu/ub/commit/4e9b8a529b0bcaada0c50a5f35094963a1705f45))


### Maintenance

- V0.3.4 ([3edb3a7](https://github.com/feimingxliu/ub/commit/3edb3a7c875b8c8dd628f8ed2b37b94902cddf92))


### Other

- Update roadmap ([0550f3a](https://github.com/feimingxliu/ub/commit/0550f3a3e25c20ec33622354cee53d1a0b713944))


### Tests

- Stabilize TUI activity width assertion ([9398e3b](https://github.com/feimingxliu/ub/commit/9398e3b2e3a52cde4f02576886e993f11312b31c))

## [0.3.3] - 2026-05-29


### Fixed

- Flatten referenced tool schemas ([3138163](https://github.com/feimingxliu/ub/commit/313816373960834670d5bdb39bf227fcd088a7f4))
- Keep default model scoped to its provider ([792e55d](https://github.com/feimingxliu/ub/commit/792e55d930a0d3c339bb3cea201cd18fe2349537))
- Persist provider with sessions ([c55af2f](https://github.com/feimingxliu/ub/commit/c55af2fd19d0d3a503d7840c3d43fba810dd2c25))
- Sync restored session mode ([5cfdfde](https://github.com/feimingxliu/ub/commit/5cfdfde42a8e4d8a6433efe585ca62ac67493ca5))


### Maintenance

- V0.3.3 ([76a59fc](https://github.com/feimingxliu/ub/commit/76a59fc1ca4da1d0f7e41c6846480842725ee0c3))

## [0.3.2] - 2026-05-29


### Added

- Add workspace instruction harness ([8ec904e](https://github.com/feimingxliu/ub/commit/8ec904e662f3615af61cd7f8f03ce058c67b31d0))
- Strengthen coding-agent guidance ([f736dc5](https://github.com/feimingxliu/ub/commit/f736dc5aea057be9f7b581907d98b40688b7aa95))
- Run init through agent ([78cd265](https://github.com/feimingxliu/ub/commit/78cd2650ea152aa1e9496ede54150bbbcc0ae4c1))


### Documentation

- Expand prompt harness roadmap ([9851fe6](https://github.com/feimingxliu/ub/commit/9851fe64d8bf2cdb959a8796438933eb9c8244ed))


### Fixed

- Avoid marking mixed tool groups failed ([1d48d74](https://github.com/feimingxliu/ub/commit/1d48d74e43006486e5048d39dbbd518ac2d7ac0d))


### Maintenance

- V0.3.2 ([2efedef](https://github.com/feimingxliu/ub/commit/2efedef9ec1500a9652add094422838e5b3a07cd))


### Tests

- Cover harness behavior regressions ([86a6bf3](https://github.com/feimingxliu/ub/commit/86a6bf37696430d43c72432642ef57ae6b667eb0))

## [0.3.1] - 2026-05-28


### Fixed

- Harden plan mode tool handling ([e7c6de8](https://github.com/feimingxliu/ub/commit/e7c6de8a5766f0d3b3d5b357883cd7fc7c215618))
- Preserve tool activity details ([6c3bdb7](https://github.com/feimingxliu/ub/commit/6c3bdb77c27b2b8c1eadaac92bff3974f8160c3a))


### Maintenance

- V0.3.1 ([b01e321](https://github.com/feimingxliu/ub/commit/b01e321d790a415fcfe7867ed62741f58084477a))

## [0.3.0] - 2026-05-27


### Added

- Clear sessions across all workspaces ([c37b069](https://github.com/feimingxliu/ub/commit/c37b069bc1bd341e7fa6b28fcf0a2fef68efcedf))
- Add multiedit tool with atomic batch semantics [V2-S3-05] ([0c13ee5](https://github.com/feimingxliu/ub/commit/0c13ee5de8b0fe0db41c3fe5dc7cb7773d750abd))
- Add tool_result snapshot tool [V2-S3-08] ([b945f6f](https://github.com/feimingxliu/ub/commit/b945f6fdae0badfbb6b4960c82243bfb20b4b9e8))
- Shell hooks at 4 lifecycle points [V2-S3-01] ([9d869d7](https://github.com/feimingxliu/ub/commit/9d869d74644f897b12c603477838120ea06f030d))
- Plan_write + plan_update_step tools [V2-S3-04] ([91a6172](https://github.com/feimingxliu/ub/commit/91a6172cb1049ce7f6e3c8305efd92dc0c0404c5))
- Hover, completion, document_symbols, rename, code_action [V2-S3-07] ([7d5f789](https://github.com/feimingxliu/ub/commit/7d5f789cb36659d30d3a7892eb19913403101d58))
- Full_max_bytes cap, custom dir, shell_metadata block [V2-S3-09] ([f13ae7b](https://github.com/feimingxliu/ub/commit/f13ae7b871dbac761d75aac79f901197b7f95693))
- Workspace + global memory with remember tool [V2-S3-02] ([ccc013a](https://github.com/feimingxliu/ub/commit/ccc013acb01e75a28d0d46456a150a9c64f61ed1))
- StreamingTool interface + bash streams partial output [V2-S3-06] ([c738ed6](https://github.com/feimingxliu/ub/commit/c738ed675e7193245c6abd2757b041120b470efa))
- Task tool dispatches sub-agents [V2-S3-03] ([9fa9591](https://github.com/feimingxliu/ub/commit/9fa959178deb8415ee396ca79989f96b1fcef848))


### CI

- Stabilize release workflows ([b6fa58b](https://github.com/feimingxliu/ub/commit/b6fa58be1e5d659068066338114c3911be488551))


### Documentation

- Sync and archive 9 V2 §3 changes ([e598c44](https://github.com/feimingxliu/ub/commit/e598c449edc00cd0fda124a297696324aee5d2c7))


### Fixed

- Handle tool-call runtime regressions ([77508d3](https://github.com/feimingxliu/ub/commit/77508d33570f802973149cfe7d50158ade36ae8f))
- Harden tool runtime boundaries ([e5466a0](https://github.com/feimingxliu/ub/commit/e5466a020ffc07243ce9fc58405bc93e3d0ff470))


### Maintenance

- V0.3.0 ([f15b263](https://github.com/feimingxliu/ub/commit/f15b263a2ecd1d3a0929ea522aa30cf550401969))


### Performance

- Defer tui startup work ([7f7f287](https://github.com/feimingxliu/ub/commit/7f7f28713a0a417f939d8c20e674b5a50fd39709))

## [0.2.7] - 2026-05-25


### Build

- One-shot make release VERSION=x.y.z ([0a3b3ce](https://github.com/feimingxliu/ub/commit/0a3b3ce7d9059f602d1c6c3fe09c7d06e9187976))


### Documentation

- Backfill v0.2.1 through v0.2.6 ([870fc5d](https://github.com/feimingxliu/ub/commit/870fc5db965845ed24250d200c973935f06fa2d3))


### Maintenance

- V0.2.7 ([5cd7b66](https://github.com/feimingxliu/ub/commit/5cd7b6650bde2717b8d3cba6bbec33c3af905fc7))

## [0.2.6] - 2026-05-23


### Tests

- Skip unstable windows large-job fixture ([2c0a699](https://github.com/feimingxliu/ub/commit/2c0a6993d46c1ced11dec3285941c173bcd8aea2))

## [0.2.5] - 2026-05-23


### Tests

- Stabilize windows rollout and job tests ([ac50fc9](https://github.com/feimingxliu/ub/commit/ac50fc9e4feaba62bed9b0a70aec9baf74f4e860))

## [0.2.4] - 2026-05-23


### Tests

- Fix windows platform workflow ([9dbbd79](https://github.com/feimingxliu/ub/commit/9dbbd794577f187d4aa87831c878ed72f23f3e4d))

## [0.2.3] - 2026-05-23


### CI

- Keep release notes outside dist ([95a6b99](https://github.com/feimingxliu/ub/commit/95a6b99d287117e74d6c3f17e96d4695894f9094))

## [0.2.2] - 2026-05-23


### CI

- Fix release workflow ([9cbf8d7](https://github.com/feimingxliu/ub/commit/9cbf8d7cfeae839f646d114f5b259371fe93c80e))

## [0.2.1] - 2026-05-23


### Fixed

- Surface error when stream ends with no reply or tool call ([d27e67a](https://github.com/feimingxliu/ub/commit/d27e67a146a2914a1c9285cc457a531ed55df865))
- Detect truncated tool-call arguments in streams ([df9fe8e](https://github.com/feimingxliu/ub/commit/df9fe8eead23d58ac352f6678ebd9056e6b2cca6))
- Collapse newlines in thinking summary so footer stays visible ([da2a12f](https://github.com/feimingxliu/ub/commit/da2a12f3c33088dda86763a1476e1fddfb949b2e))
- Retry with recovery prompt when reasoning exhausts output budget ([1e71448](https://github.com/feimingxliu/ub/commit/1e71448e353956d1fdc83c40d7feb69b0d5745fc))

## [0.2.0] - 2026-05-23


### Added

- Reconnect MCP tool servers ([8292b05](https://github.com/feimingxliu/ub/commit/8292b059d9a74a2cd2227ace101586d5a824c32d))
- Bound background job lifecycle ([d91b015](https://github.com/feimingxliu/ub/commit/d91b01530910bfa1dc6b673daf6bcefec66aac04))
- Add fuzzy filtering to sessions picker ([8ecf489](https://github.com/feimingxliu/ub/commit/8ecf489bafba602a9cd40ceba08427b8ed39514f))
- List sessions across workspaces ([03d09cc](https://github.com/feimingxliu/ub/commit/03d09ccbb46c5c612eba4637019341258396798e))
- Add retry slash command ([535fc50](https://github.com/feimingxliu/ub/commit/535fc500265e98ae1a96740bf4c408b1e9f0ab6c))
- Run doctor from slash command ([a2bcb07](https://github.com/feimingxliu/ub/commit/a2bcb077a7b66ce26723c3a5b96eb4a10035e9c2))
- Add toast feedback layer ([7687bbc](https://github.com/feimingxliu/ub/commit/7687bbcbf736b7c62971d9d7b08e521235b195a1))
- Add status bar cheatsheet entry ([bb0d3aa](https://github.com/feimingxliu/ub/commit/bb0d3aaa490240a699750ea5a8923dd54998862e))
- Copy transcript messages to clipboard ([4933542](https://github.com/feimingxliu/ub/commit/4933542b631dbd6daa911a05f13f6477f366b4f0))
- Search rollout text across sessions ([7d3b9aa](https://github.com/feimingxliu/ub/commit/7d3b9aad09f8d1060bcea78238c90d9391539a4b))
- Warn on narrow startup terminals ([7c6d256](https://github.com/feimingxliu/ub/commit/7c6d256623cf1f4b68828c08cec2c1b9fa3b4901))
- Add JSON doctor output ([53cc1d9](https://github.com/feimingxliu/ub/commit/53cc1d91ac40db0c93e6607ff33e30ddb788a67b))
- Bound sessions search and tidy doctor JSON ([9403750](https://github.com/feimingxliu/ub/commit/9403750eb077976162b78e7f899b7f15ec0d0d30))


### Build

- Enforce fmt + vet on commit, full check on push ([c269749](https://github.com/feimingxliu/ub/commit/c269749557fbaa743b992358cd1cebf845cdfc9b))
- Force CGO_ENABLED=1 for race tests in make check ([d46fee1](https://github.com/feimingxliu/ub/commit/d46fee18a9c8c6721c9bf0001c8e586585237ab2))
- Add changelog release notes automation ([7946356](https://github.com/feimingxliu/ub/commit/794635675310b4fac51a88362810e3be4fdfae62))
- Add release signing and SBOMs ([417304d](https://github.com/feimingxliu/ub/commit/417304dc3447920dd93ac4f75c7d46aaccc36a5d))


### CI

- Collapse CI workflow to a single \`make check\` invocation ([19f4b8d](https://github.com/feimingxliu/ub/commit/19f4b8d50285625c4d47c1bded08601d067574fa))
- Add Windows platform validation ([541b3a4](https://github.com/feimingxliu/ub/commit/541b3a4ead29c3b8479b30fbe4ea457c49618718))


### Documentation

- Plan tool streaming, output spillover, job lifecycle ([962374b](https://github.com/feimingxliu/ub/commit/962374b6ab05d03aeb82853b58bb76c610d21723))
- Add contributor guide ([e48f487](https://github.com/feimingxliu/ub/commit/e48f487ae45773c93c0f040638a88b1e0af72273))
- Add issue and pull request templates ([85c5897](https://github.com/feimingxliu/ub/commit/85c5897b3cac4d16ac87c804222559ded5192322))
- Simplify quick install path ([89ad8a5](https://github.com/feimingxliu/ub/commit/89ad8a5b34252a206a078ff79041bbf23d070c96))
- Scope blacklist as defense-in-depth, not a sandbox ([807cd35](https://github.com/feimingxliu/ub/commit/807cd35ad2b8b0f6d0878e29139e27540f6ac293))
- Record breaking workspace key and review fixes ([219ea07](https://github.com/feimingxliu/ub/commit/219ea078d85f09b3d40a3655b897d2d5ec3ae604))


### Fixed

- Normalize session workspaces ([2bad569](https://github.com/feimingxliu/ub/commit/2bad5692f19ddcea00fcd3fb414ce3f6d60510da))
- Only reconnect on transport errors ([f724463](https://github.com/feimingxliu/ub/commit/f724463d841a5b470d58d8b152638f29d97afe85))
- Release slot inside lock, pass real context to Shutdown ([6e3e789](https://github.com/feimingxliu/ub/commit/6e3e7897f297c848af71c8317363c557f51c4b20))
- Async clipboard/doctor, dedupe toasts, hit-test help via metadata ([5fe3e42](https://github.com/feimingxliu/ub/commit/5fe3e42d107c269fa42a9554977b8dc19f55212e))


### Maintenance

- Gofumpt-sort imports in internal/tui/model.go ([5c5232f](https://github.com/feimingxliu/ub/commit/5c5232f47ef11844bf1a9243233faff9450aec18))


### Refactoring

- Header-only timeout; drop native ollama provider ([afffbee](https://github.com/feimingxliu/ub/commit/afffbee17159a62280cb6bfc3b3364362eb17619))


### Tests

- Fuzz permission blacklist normalization ([800a3b3](https://github.com/feimingxliu/ub/commit/800a3b38618a53b316f3e84421a52aebffc8f939))

## [0.1.2] - 2026-05-22


### Added

- Restore session activity, detect terminal size, fix reasoning ([7cc983a](https://github.com/feimingxliu/ub/commit/7cc983ac2ff8448c58e05bb2727cb13f61039918))
- Raise default max turns to 50 and make it configurable ([ae835b2](https://github.com/feimingxliu/ub/commit/ae835b296fbb90b293475b59506c228654243456))
- Ask host before falling through to no-tool finalize ([9c84eb7](https://github.com/feimingxliu/ub/commit/9c84eb79823eb3f5df2d8d2ba2e47e7b205eed0a))


### Documentation

- Render hero banner as SVG to fix centering ([70475e6](https://github.com/feimingxliu/ub/commit/70475e6c619bd6faf6d0f80446a0ee18a77ff519))
- Add V2 roadmap and cross-link from V1 + READMEs ([81ae687](https://github.com/feimingxliu/ub/commit/81ae687f854a8bedf80c47e551630b719e582a6a))


### Fixed

- Preserve paragraph breaks in streamed thinking deltas ([c85a532](https://github.com/feimingxliu/ub/commit/c85a5321fc4c6b35fac2992bf57958171e70fda3))
- Namespace resume activity groups by turn ([004d2f8](https://github.com/feimingxliu/ub/commit/004d2f8e588a0647bb8177f410a6d65cae69f5e4))

## [0.1.1] - 2026-05-21


### Documentation

- Revamp README for open-source release ([ef07131](https://github.com/feimingxliu/ub/commit/ef071319ae61ff99de0f43ac6a2a8e232791ac1a))
- Embed demo recording into README ([8054931](https://github.com/feimingxliu/ub/commit/80549319f1d14582396311aa0ba38be27d679781))


### Fixed

- Expand tabs before wrapping TUI message lines ([9ec1c12](https://github.com/feimingxliu/ub/commit/9ec1c12ebf03e3150b91167efa4fd455b82c9dfa))
- Skip startup session picker on plain ub launch ([695645b](https://github.com/feimingxliu/ub/commit/695645bf389509c41d67c5d60d77fa889b76e721))


### Maintenance

- Add make install target ([494e7fa](https://github.com/feimingxliu/ub/commit/494e7fa96a713493bd036f80e1c16cbd1c5b54e0))

## [0.1.0] - 2026-05-21


### Added

- Add OpenAI chat provider ([ec503fd](https://github.com/feimingxliu/ub/commit/ec503fd9ee4bc60b2f7a3e9dd310c17ce0aa8711))
- Add compat and ollama providers ([5274afd](https://github.com/feimingxliu/ub/commit/5274afd296308e4394686ccedc783155d91a56a2))
- Add profiles and doctor ([659b3f2](https://github.com/feimingxliu/ub/commit/659b3f2e9ef271d7a0d22dec50ecf9bac22b7d42))
- Complete chat sessions ([5113fd3](https://github.com/feimingxliu/ub/commit/5113fd39d07a5a423b0028cdae03781c4619b0b0))
- Add session deletion commands ([a50cfb5](https://github.com/feimingxliu/ub/commit/a50cfb56942b76964888da89c7033198b5080986))
- Add background job tools ([9a0f55b](https://github.com/feimingxliu/ub/commit/9a0f55b54c2475ae530fafdaf34c2bbfe60a258c))
- Add permission manager ([e2ed15c](https://github.com/feimingxliu/ub/commit/e2ed15c84800600522c60f88743559ad7fdbfeb0))
- Add agent loop ([29ae24a](https://github.com/feimingxliu/ub/commit/29ae24ae97f2c4e1435ba523eaabe17215d02a40))
- Show agent activity stream ([072aacf](https://github.com/feimingxliu/ub/commit/072aacf0fe476dad02a50f9d0da0f4cd16062264))
- Add reasoning effort control ([0a9aed6](https://github.com/feimingxliu/ub/commit/0a9aed65e7995e0114a3368994b844a5a392b9c0))
- Add startup cleanup maintenance ([38a5c7e](https://github.com/feimingxliu/ub/commit/38a5c7ef9dda683e8da582393521d6cbfbfb2c74))
- Add TUI message queue ([e9fb985](https://github.com/feimingxliu/ub/commit/e9fb9857c81bb2b617037e9cabc67d4259b66d89))
- Improve context compaction and tool output handling ([a4e830f](https://github.com/feimingxliu/ub/commit/a4e830f3a89daa050d63ee811c3e866ad37c58e9))
- Split thinking and tool activity groups ([13523a7](https://github.com/feimingxliu/ub/commit/13523a7c838ee61862deafda0d52a957dacd5bb6))
- Add new session slash command ([d191450](https://github.com/feimingxliu/ub/commit/d19145096c0cb35cbde3808b4dbbee617011d3d8))
- Add two-stage tool diff expansion ([a280468](https://github.com/feimingxliu/ub/commit/a28046843cd8d34467c47da89a88ebe9679560a8))
- Improve TUI session and local input controls ([a0150ef](https://github.com/feimingxliu/ub/commit/a0150ef5a6faffa2597175ace66562a1e8ef4981))
- Add TUI provider switching ([dc23f49](https://github.com/feimingxliu/ub/commit/dc23f49c822e5134bcef3902a1caeed2469e276f))
- Add TUI run indicator and enable mouse scrolling ([9a8cd43](https://github.com/feimingxliu/ub/commit/9a8cd43b3cd3dd7ecaf95b3cf7ea57351ca66be7))


### Documentation

- Initial requirements, design, and roadmap ([37f6434](https://github.com/feimingxliu/ub/commit/37f6434332f53536469ca7f9fa4a364dedb3b5d7))
- Update execution modes and archive config loader ([7751a74](https://github.com/feimingxliu/ub/commit/7751a74c5d861535bda77aa7fb6cad5cbfb27a4f))
- Update repository agent guidance ([9b023be](https://github.com/feimingxliu/ub/commit/9b023be468d497b156ee9992f6664cd7aadc9b72))


### Fixed

- Redact sensitive provider headers ([e3c0234](https://github.com/feimingxliu/ub/commit/e3c0234bc0e29e6f69dc3480b7b21c6db6a0595d))
- Honor configured chat provider ([1e05d1d](https://github.com/feimingxliu/ub/commit/1e05d1debb8f2097921d48d236ec4035cd532963))
- Improve TUI navigation and session UX ([692e76f](https://github.com/feimingxliu/ub/commit/692e76f0bf5e40a617b1eea28ddb235effa4c150))
- Accept numeric strings in tool args ([c47b4be](https://github.com/feimingxliu/ub/commit/c47b4bec54435ab0ed4ef42f1e0b29b656f3bf01))
- Inject runtime workspace context ([0f19a57](https://github.com/feimingxliu/ub/commit/0f19a570c4c0911572592ceeebcee51bf2c86580))
- Accumulate TUI thinking deltas ([67ed23a](https://github.com/feimingxliu/ub/commit/67ed23aa1b709d93108bc9f32ab18d3d5e693bc4))
- Tolerate tool argument type jitter ([94f55b4](https://github.com/feimingxliu/ub/commit/94f55b4134b85119ec79ed6ca6b8f8d26e6ef646))
- Reduce TUI input redraw jitter ([4a444bd](https://github.com/feimingxliu/ub/commit/4a444bd09051bd36255d8aa269d22ca6830e5986))
- Prevent read tool from reading directories ([9fa5e29](https://github.com/feimingxliu/ub/commit/9fa5e29331e3ec87b3ceef82b40f07093af5a507))
- Clean up TUI tool details ([cd4861b](https://github.com/feimingxliu/ub/commit/cd4861b245c8dd479d4afb8fbf10e59ba6c8d12f))
- Correct TUI IME cursor handling ([aa002f1](https://github.com/feimingxliu/ub/commit/aa002f1c65a68830a57c7d5807dded06c84f80fc))
- Prevent TUI status bar shrink loop ([39881dd](https://github.com/feimingxliu/ub/commit/39881dd307e74aec98ccc808643a817928c08cf2))
- Align provider doctor checks with SDK clients ([dab7e16](https://github.com/feimingxliu/ub/commit/dab7e16a331dc8506de8edbbbcb497dd26926814))


### Maintenance

- Init repo ([c958bea](https://github.com/feimingxliu/ub/commit/c958bea63bb8c4d60e5229d8b8c403cdb10663c5))
- Add AGENTS.md and ignore tool-local dirs (.claude/.codex/.opencode) ([6180ff8](https://github.com/feimingxliu/ub/commit/6180ff855ad250475cfa07ad0184d5c3d2463cea))
- Upgrade direct dependencies and tui controls ([a049e69](https://github.com/feimingxliu/ub/commit/a049e69b8f176a103f5eb72e5419f0a3996952f1))


### Other

- [I-01] scaffold cobra CLI with placeholder subcommands

- go module github.com/feimingxliu/ub on Go 1.26
- cmd/ub: thin entry calling internal/cli.Execute
- internal/cli: cobra root command + run/config/sessions placeholders
  that return iteration-tagged "not implemented" errors so future
  iterations have explicit hand-off points
- Version() reads runtime/debug.ReadBuildInfo, preferring Main.Version
  for tagged releases and falling back to dev+sha for git checkouts
- Makefile with build/test/vet/fmt/lint/tidy targets (gofumpt opt-in,
  gofmt fallback)
- .github/workflows/ci.yaml: vet + test -race + build + gofumpt gate

Verification: go build, ub --version, ub {run,config,sessions} --help
all green; go test ./... and go vet ./... pass. ([48c7e9a](https://github.com/feimingxliu/ub/commit/48c7e9a40393b130b483f6e4acb6877a74683a1d))
- [I-02] add YAML config loader with layered merge, env expansion, JSON Schema ([f123eaa](https://github.com/feimingxliu/ub/commit/f123eaa2cba147cc38479ec0750715861dfbe8da))
- [I-03/I-04] add store and logging ([7407002](https://github.com/feimingxliu/ub/commit/7407002ee496293ba5cffafd7fca3499ca615eba))
- [I-05] add message types ([1bd27fc](https://github.com/feimingxliu/ub/commit/1bd27fced41fde16033ff6aa79db199183bf5aab))
- [I-06] add HTTP VCR recorder ([b1bd0e4](https://github.com/feimingxliu/ub/commit/b1bd0e4884165ec578572f8eb24986b81ebbdebd))
- [I-07] add provider fake chat ([9e980b3](https://github.com/feimingxliu/ub/commit/9e980b3d576587d25b2cb9b032603334a5ab0c0d))
- [I-08] add Anthropic provider ([050df9c](https://github.com/feimingxliu/ub/commit/050df9c41816ee6ededf14b3bc5f368cd4dcda62))
- [I-09] add rollout events ([4238589](https://github.com/feimingxliu/ub/commit/4238589a9f93c7dc7fea9138d4727381f2a4ae37))
- [I-10] add Anthropic streaming ([cd77579](https://github.com/feimingxliu/ub/commit/cd7757928e44efe936f048982bf4efa9016d50c2))
- [I-15] add tool interface and registry

Introduce the local-tool foundation that fs/search/bash tools and the
agent loop will share: Tool interface, Risk taxonomy (safe/write/exec),
PreviewableTool optional interface, and a name-keyed Registry with
duplicate detection and sorted iteration.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com> ([70e4d0a](https://github.com/feimingxliu/ub/commit/70e4d0ad27c5ea25bd08e2d1bfa7167885289ac3))
- [I-16] add fs tools (read/ls/glob/write/edit)

Five workspace-scoped file tools share a common sandbox: every input
path is cleaned and rejected if it escapes the workspace root passed
to fs.Register. write and edit implement PreviewableTool so the
dispatcher can render unified diffs before applying changes; edit
also double-reads the file at execution time to guard against TOCTOU
between preview and write.

Dependencies: doublestar/v4 (glob), go-udiff (unified diff).

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com> ([990fefd](https://github.com/feimingxliu/ub/commit/990fefd434fb8b80167851e2ffddfac46a48935a))
- [I-17] add grep search tool

A regex-over-workspace tool with two interchangeable backends sharing
a single output format (path:line:match, paths relative to root,
sorted by path then line):

- goBackend (default): WalkDir + regexp + bufio.Scanner. Binary files
  detected by NUL byte in the first 8KB are skipped; matched lines
  past 2KB are truncated.
- rgBackend: shells out to ripgrep with --line-number --no-heading
  --color=never --no-messages, exposed through a commandRunner
  interface so tests can assert argv without depending on rg being
  installed. Wired but not selected in V1; goBackend keeps CI
  reproducible across machines.

Also extracts the workspace-path sandbox into tool.Resolve so future
search/bash/job tools share the same rule as fs.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com> ([ab315f6](https://github.com/feimingxliu/ub/commit/ab315f6ffb8e42c84503b61509772deea7f811f4))
- [I-18] add bash shell tool

A one-shot shell executor (no background jobs, no permission gating)
that runs a command through /bin/sh -c with these guarantees:

- Workspace sandbox: cwd resolved via tool.Resolve; escapes are
  rejected before any process starts.
- Timeout: default 120s, overridable per call. On expiry the child's
  process group is signalled SIGTERM, then SIGKILL after 2s, so any
  grandchildren are reaped too.
- Output capture: stdout/stderr each capped at 32KB in the returned
  Result.Content; the true byte count is reported in a truncation
  footer when the cap is exceeded.
- Closed stdin (os.DevNull) so reads return EOF immediately.

POSIX-only (bash_unix.go uses Setpgid + syscall.Kill on -pid);
bash_windows.go is a stub that returns a not-supported error to keep
cross-compile clean. Permission/blacklist enforcement and background
jobs are intentionally deferred to I-20 / I-19.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com> ([3e2ac53](https://github.com/feimingxliu/ub/commit/3e2ac53d8378c64bc7df5e22b4547352c74f4002))
- [I-22] add Bubble Tea TUI skeleton ([9c403c2](https://github.com/feimingxliu/ub/commit/9c403c24ac2a60c7e3aac6c170fcecd06757ca66))
- [I-23] connect TUI to streaming agent ([0715b5c](https://github.com/feimingxliu/ub/commit/0715b5c103e4594df7787b4bc18d4aa9d24b03a2))
- [I-24] add TUI permission modal ([31478c5](https://github.com/feimingxliu/ub/commit/31478c5b0a9818b31a28b7ccf5189ec8971ed153))
- [I-25] add TUI rich diffview ([5d69e95](https://github.com/feimingxliu/ub/commit/5d69e955fd71a20a79d0ffac8dcf38bacd1570d2))
- [I-26] add TUI slash commands ([80656d2](https://github.com/feimingxliu/ub/commit/80656d2092e55f97208349000f2f3e63532e076d))
- [I-24] improve permission approval selection ([5f16ba4](https://github.com/feimingxliu/ub/commit/5f16ba4c45e274698a4fda303aa61de72a6501ca))
- [I-20] rename execution modes ([5d37c75](https://github.com/feimingxliu/ub/commit/5d37c7555445f63621329555b31e82a7498c6443))
- [I-26] improve TUI approval and model controls ([8074514](https://github.com/feimingxliu/ub/commit/80745142474496d1d04241bb80a1e4d9fd7027fd))
- [I-26] improve TUI presentation blocks

Add built-in TUI styles, Markdown message rendering, grouped collapsible activity blocks, and segmented status/picker styling.

Fix activity updates while permission modals are open and disable synthetic TUI event timeouts.

Validation: go test ./... ([7a12297](https://github.com/feimingxliu/ub/commit/7a122978215e5fa41acaf7cc5630d389ef16dded))
- [I-27] add token estimation ([f7ad6e0](https://github.com/feimingxliu/ub/commit/f7ad6e0a9c006325e6ededb8a61e537df1731d25))
- [I-28] add automatic context summary ([fd1de8b](https://github.com/feimingxliu/ub/commit/fd1de8b8d525f20c3572f177f7912277d0f94cb1))
- [I-28] add manual compact and context status ([6eb7b4a](https://github.com/feimingxliu/ub/commit/6eb7b4a2cc4ef66382b6a5c115dd1afdc5c6775e))
- [I-21] finalize after tool loop limit ([399373d](https://github.com/feimingxliu/ub/commit/399373d8e6f6349c825cd21b8ab1217fcdcccd17))
- [I-28] clarify context status resets ([7d6030c](https://github.com/feimingxliu/ub/commit/7d6030cda77888d0d69473184c80b007b5fad524))
- [I-28] add configurable model context window ([e159759](https://github.com/feimingxliu/ub/commit/e159759e02d7c6e22c6055e8073febdd328e56d0))
- [I-29-I-32] add MCP and LSP integrations ([187ecab](https://github.com/feimingxliu/ub/commit/187ecab89f8aba75bb7a787f63eb802750db9532))
- [I-33] persist TUI session mode resume ([bf9e3a1](https://github.com/feimingxliu/ub/commit/bf9e3a152d859771bc8328e6727f32eddd815dd4))
- [I-34] add rollout show command ([fc1683b](https://github.com/feimingxliu/ub/commit/fc1683b446c3025449cbed1fa463f5ff6215add8))
- [I-35] add release docs and workflow ([7d68a06](https://github.com/feimingxliu/ub/commit/7d68a0658686cf24da378e38057fc08f24208afc))


### Style

- Polish tui visual design ([0ebfd1a](https://github.com/feimingxliu/ub/commit/0ebfd1aab2ebbe4cae973d203fc2e58d524a0401))
- Run gofumpt ([d7181d4](https://github.com/feimingxliu/ub/commit/d7181d421811402b64eaaa3df20fb44ada2c6eca))

<!-- generated by git-cliff -->

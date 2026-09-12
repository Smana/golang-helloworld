# Changelog

This file tracks all notable changes to the Image Gallery project.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.0.0](https://github.com/Smana/image-gallery/compare/v1.7.7...v2.0.0) (2026-09-12)


### ⚠ BREAKING CHANGES

* **cli:** the binary is /app/image-gallery (was /app/server) and cmd/server is removed.
* **upload:** image.uploads.total, image.deletions.total, image.cache.hits/misses, settings.read/write.total and settings.cache.hits/misses are replaced by image.uploads, image.deletions, cache.lookups and settings.operations.
* **storage:** the storage.operations.total, storage.bytes.transferred and image-gallery/service/storage span names are replaced by storage.operations, storage.transferred and storage.<op> CLIENT spans.
* **observability:** http.server.request.count and http.server.response.size are replaced by the semconv http.server.request.duration count and http.server.response.body.size.

### Features

* **cli:** one image-gallery binary with serve as the default command ([6afc7a6](https://github.com/Smana/image-gallery/commit/6afc7a60ed2112033705ddc2974fe5112b47da65))
* **db:** processing status for asynchronous thumbnails, and the demo_controls table ([9b8b204](https://github.com/Smana/image-gallery/commit/9b8b204b61da7ea86ab8f838dd73c604280494f3))
* **demo:** make the demo control endpoints switchable ([22ec242](https://github.com/Smana/image-gallery/commit/22ec242e0a0ae05d684e74b3671d51e87cef168c))
* **demo:** switchable fault injection for the observability demo ([9c121dc](https://github.com/Smana/image-gallery/commit/9c121dcf8d75991c8e774a86dfd2542e98fc6632))
* image-gallery v2 — worker, GCS, demo controls, load generator, telemetry contract ([8f99b7a](https://github.com/Smana/image-gallery/commit/8f99b7a03ae830bbbbd4ef639ec269cb1677d171))
* **loadgen:** open-loop load generator with demo scenarios ([428b3c9](https://github.com/Smana/image-gallery/commit/428b3c9e49f028177f1f7cd61067117f053d7d7d))
* **observability:** current OTel SDK, span-drop counters and Go runtime metrics ([e6e9bf3](https://github.com/Smana/image-gallery/commit/e6e9bf32c42cecdb49257bc7bb31dd577ec6f62f))
* **queue:** stream jobs over Valkey with trace propagation, retries and dead-lettering ([5260aa4](https://github.com/Smana/image-gallery/commit/5260aa4ec5f5978fb0bd0fa804558392bf7049dc))
* **storage:** one object-store interface over S3 and GCS ([07a9a8c](https://github.com/Smana/image-gallery/commit/07a9a8c73e65f7335da87f01df2d5cad595bcf71))
* **upload:** hand images to the worker queue and serve thumbnails ([5b9860d](https://github.com/Smana/image-gallery/commit/5b9860da337c6a3bbbe9894f9ae2a658128473c5))
* **worker:** process images from the queue into thumbnails and metadata ([37dc5a5](https://github.com/Smana/image-gallery/commit/37dc5a5167f92f79023ea6fb6f71562e0f99e0e6))


### Bug Fixes

* **api:** remove the unroutable image view URL ([dbc8680](https://github.com/Smana/image-gallery/commit/dbc86808c593dbb39b6a4b0bf5ce822b3338732a))
* **app:** start the web role without Valkey instead of crashing ([6a2b9bd](https://github.com/Smana/image-gallery/commit/6a2b9bd5c1629a4b02621f98f7218aa56f38957b))
* **cache:** drop the single-image cache on worker status writes, not just lists ([6a3bf67](https://github.com/Smana/image-gallery/commit/6a3bf6783107e24c5d90babd66d1d78684b566b5))
* **config:** validate GCS bucket names at startup ([4ba4bbb](https://github.com/Smana/image-gallery/commit/4ba4bbbd084106815f4e4e3062edec46e0618b9e))
* **db:** check RowsAffected errors and document Update's status-field exclusion ([825bab5](https://github.com/Smana/image-gallery/commit/825bab59e4433488b3e71a71971fa43ee893f8c0))
* **db:** rehash the migration directory after migration 004 ([13502bc](https://github.com/Smana/image-gallery/commit/13502bccf67ef15acbfa9dd77f87d627e8746c49))
* **demo:** cap worker delay below the queue claim timeout ([a2ef90c](https://github.com/Smana/image-gallery/commit/a2ef90c6b45518643e96498b08217e476d5f2710))
* **demo:** record every injected fault on the span, not just the last ([baa3cfc](https://github.com/Smana/image-gallery/commit/baa3cfc56ffe35e51cf0cde437ff4e6300b62ee8))
* **demo:** stop the slow-query fault from exhausting the connection pool ([b99a4f9](https://github.com/Smana/image-gallery/commit/b99a4f9ef4b82f080b13f4e34d4d4f7cdd8c3e2a))
* **deps:** clear nine advisories, which raises the Go floor to 1.26 ([eb626d1](https://github.com/Smana/image-gallery/commit/eb626d1646b5821c82557c97c8b1bc0f77e48411))
* **e2e:** unregister the queue gauges as the worker now does ([2eebbce](https://github.com/Smana/image-gallery/commit/2eebbce40d9610039083d2f255a9cc8535cdb127))
* **image:** load tags on GetByID and implement image stats ([cc1c282](https://github.com/Smana/image-gallery/commit/cc1c2829c29eac956d34e242ff504c1103e0d442))
* **observability:** continue incoming traces and key HTTP telemetry by route pattern ([9b66d7d](https://github.com/Smana/image-gallery/commit/9b66d7d6fa7e1df9aa96e9325c834494b7a212c1))
* **observability:** report the built version instead of a stale literal ([03b9f0c](https://github.com/Smana/image-gallery/commit/03b9f0c1ad41d3d14c92dc0fd8b2aabe15ee26b5))
* **queue:** bound the dead-letter hook so it cannot block consumer shutdown ([5e971f7](https://github.com/Smana/image-gallery/commit/5e971f72d1804ad68a632eb9b83e1a25249bbc3c))
* **security:** bound multipart parsing and make the rune conversion safe ([f6d3281](https://github.com/Smana/image-gallery/commit/f6d328117e65426c6d51acc3579c5a0565f37463))
* **storage:** persist and verify objects via the MinIOClient fallback ([536ed53](https://github.com/Smana/image-gallery/commit/536ed536038334d0d9a8b07f2d413e2ead1695e3))
* **storage:** time the whole object read, not just opening it ([355fd6e](https://github.com/Smana/image-gallery/commit/355fd6ea171c17e6f41eb809c576bc4a402d2153))
* **upload:** mark images failed when no queue is configured ([18e2cce](https://github.com/Smana/image-gallery/commit/18e2ccecaaf7313e706f0d3e35ca2e96aff9f124))
* **upload:** report a failed status update after a failed enqueue ([5ece5af](https://github.com/Smana/image-gallery/commit/5ece5afbde54b660f7c98a5f32cb0df311d4a140))
* **web:** parent view-image spans to the incoming request ([caba4b2](https://github.com/Smana/image-gallery/commit/caba4b2cb47f16892775d5e50cf79ee93618c2da))
* **worker:** reject oversized images before decoding them ([bde3666](https://github.com/Smana/image-gallery/commit/bde36669340faa1becaa23c49acc809d45096217))
* **worker:** unregister the queue gauges before closing their client ([00a5160](https://github.com/Smana/image-gallery/commit/00a5160954c79143f9fb131c5a781dd351a4e7d2))


### Documentation

* add the worker and queue to the architecture, drop dead targets and routes ([c210a29](https://github.com/Smana/image-gallery/commit/c210a299f044384da4b3104b3b8891e0e907ca6e))
* v2 roles, storage providers, telemetry contract, demo controls and load generator ([174728d](https://github.com/Smana/image-gallery/commit/174728dc2ece0a5479e19adcad0db6f938d11c4a))


### Code Refactoring

* clear the pre-existing goconst, gocritic, prealloc and staticcheck findings ([20f9293](https://github.com/Smana/image-gallery/commit/20f92933e397580a83c5406bd91f3bd5d3ae34cc))
* **loadgen:** route span attrs through constants, dedup ID parsing ([78637f7](https://github.com/Smana/image-gallery/commit/78637f7ad062cfb3d8f5fe401655e56d2367d69a))
* name the duplicated format strings and split validateStorage ([7a0e0d1](https://github.com/Smana/image-gallery/commit/7a0e0d1b7f0f32d1eab8ca92e3491edf4bcd075c))
* remove dead fallback and duplicated sampler parsing ([9d6a0bb](https://github.com/Smana/image-gallery/commit/9d6a0bb652e08acb71b29f2ccfeeec72b91fbc4c))
* **storage:** one storage service over ObjectStore, uniform storage telemetry ([efcb34e](https://github.com/Smana/image-gallery/commit/efcb34e487f2981231394284c2ca60aaaea94278))


### Continuous Integration

* correct the trivy-action tag form so the release job can resolve it ([e636188](https://github.com/Smana/image-gallery/commit/e636188ad0e88064dd55573f0e02666ffc79abf9))
* correct the trivy-action tag form so the release job can resolve it ([3009f44](https://github.com/Smana/image-gallery/commit/3009f447bcf0144db7f39de1fef2c4a69d1059aa))
* lint with a pinned golangci-lint v2, scoped to changed code ([1002bc2](https://github.com/Smana/image-gallery/commit/1002bc25fc6fdce59e68308df6c049f62697fcc9))
* pin the Dagger Go image and golangci-lint instead of floating both on latest ([0a839dd](https://github.com/Smana/image-gallery/commit/0a839ddd1e7461f2a091db2f0e7203a0282a4210))
* pin the Go image for the lint step only, not for every Dagger call ([bd13b85](https://github.com/Smana/image-gallery/commit/bd13b85fca397e349186cb1f102d0003a48e8b17))
* run lint, tests and govulncheck on a pinned Go, and set up Go for releases ([e5c30fe](https://github.com/Smana/image-gallery/commit/e5c30fe1108a7e0f3f39ef5d4b8fa8add704e187))

## [1.7.7](https://github.com/Smana/image-gallery/compare/v1.7.6...v1.7.7) (2025-11-02)


### Bug Fixes

* **storage:** add file size parameter to Store interface and update t… ([4a6bc41](https://github.com/Smana/image-gallery/commit/4a6bc412dae1f8e4252c56ef55353ee74165bec4))
* **storage:** add file size parameter to Store interface and update tests ([74ebc1e](https://github.com/Smana/image-gallery/commit/74ebc1e51e179c38edc18f2839b896ef26df9fec))

## [1.7.6](https://github.com/Smana/image-gallery/compare/v1.7.5...v1.7.6) (2025-11-02)


### Bug Fixes

* **otel:** fix code formatting in observability provider ([5dcbf41](https://github.com/Smana/image-gallery/commit/5dcbf419b87657ed32e18e53fa8e74cb1121d515))
* **otel:** limit span queue size to prevent OOMKills under sustained … ([ee24944](https://github.com/Smana/image-gallery/commit/ee24944f5df1048ce924749f431dd38570a74c1d))
* **otel:** limit span queue size to prevent OOMKills under sustained load ([40710fd](https://github.com/Smana/image-gallery/commit/40710fd73b0550698bc904422b653892bf1ac846))

## [1.7.5](https://github.com/Smana/image-gallery/compare/v1.7.4...v1.7.5) (2025-11-02)


### Bug Fixes

* **handlers:** fix code formatting in upload handler ([c28c1af](https://github.com/Smana/image-gallery/commit/c28c1aff066b1145e4e232d957a7ded5e47f99cc))
* **upload:** reduce ParseMultipartForm memory buffer to prevent OOMKills ([18477a1](https://github.com/Smana/image-gallery/commit/18477a19ba95b5cdd54dbb23b3f730093205fb37))
* **upload:** reduce ParseMultipartForm memory buffer to prevent OOMKills ([6391248](https://github.com/Smana/image-gallery/commit/6391248f1cec9907b95bbf2c00e5da36001752ba))

## [1.7.4](https://github.com/Smana/image-gallery/compare/v1.7.3...v1.7.4) (2025-11-02)


### Bug Fixes

* **database:** increase connection pool to handle benchmark load ([7de73d4](https://github.com/Smana/image-gallery/commit/7de73d418ea32f8f50444b4b51d6ad2412739e4f))
* **health:** remove S3 from readiness check to prevent timeout failures ([e6341e7](https://github.com/Smana/image-gallery/commit/e6341e7e56d93121a228478cb4b0a0a34479bc5c))
* **health:** remove S3 from readiness check to prevent timeout failures ([23d3a58](https://github.com/Smana/image-gallery/commit/23d3a582ae71a499f9e36977da426320e0841633))

## [1.7.3](https://github.com/Smana/image-gallery/compare/v1.7.2...v1.7.3) (2025-11-02)


### Bug Fixes

* **database:** eliminate json_agg memory explosion causing immediate … ([78efae2](https://github.com/Smana/image-gallery/commit/78efae2bb2a7d15c1fd967a016514138e28abff3))
* **database:** eliminate json_agg memory explosion causing immediate OOMKills ([c82ff39](https://github.com/Smana/image-gallery/commit/c82ff39847036b625ae51a7f431f197374852c8a))
* **database:** remove deprecated json_agg code and fix test helper ([c75f4ac](https://github.com/Smana/image-gallery/commit/c75f4ac793b0ee8f98a2aa0eaf5a56eab9ff69a6))

## [1.7.2](https://github.com/Smana/image-gallery/compare/v1.7.1...v1.7.2) (2025-11-02)


### Bug Fixes

* **database:** add connection pool limits to prevent resource exhaustion ([37b13df](https://github.com/Smana/image-gallery/commit/37b13dfbf1156849ba31969b22bf91b369769c78))
* **database:** add connection pool limits to prevent resource exhaustion ([5e09eb7](https://github.com/Smana/image-gallery/commit/5e09eb7990bf6735d05293a5e4940af64020d449))
* **database:** fix code formatting in connection pool configuration ([d4e03e3](https://github.com/Smana/image-gallery/commit/d4e03e39be9b46d0500356ec2b0b8c05aff9d780))

## [1.7.1](https://github.com/Smana/image-gallery/compare/v1.7.0...v1.7.1) (2025-11-02)


### Bug Fixes

* **handlers:** fix database connection leak in slow query scenario ([5dd6b3e](https://github.com/Smana/image-gallery/commit/5dd6b3ec21af54df69f919d84ab25733ae80a779))
* **handlers:** fix database connection leak in slow query scenario ([f3ae6aa](https://github.com/Smana/image-gallery/commit/f3ae6aa7df4cddae12361763d94a5025ea606c6a))

## [1.7.0](https://github.com/Smana/image-gallery/compare/v1.6.1...v1.7.0) (2025-11-02)


### Features

* **config:** add S3 sync on startup configuration option ([d59745c](https://github.com/Smana/image-gallery/commit/d59745c9e2bf93f5b3ebefcdc613962814846ce5))
* **server:** add automatic memory limit configuration ([65d844c](https://github.com/Smana/image-gallery/commit/65d844cee03523486cc8b9982b3d267b4e521278))
* **server:** implement S3 bucket sync and automemlimit initialization ([ef005e2](https://github.com/Smana/image-gallery/commit/ef005e2f98577f5354aaef704a0ae2475345a21d))
* **settings:** set default background image with 40% opacity ([9dd4e13](https://github.com/Smana/image-gallery/commit/9dd4e131b76b116ef60ac4a137102774eff6dfca))


### Bug Fixes

* **upload:** critical memory leak fixes to prevent OOMKills ([316a264](https://github.com/Smana/image-gallery/commit/316a2644f47e04225acb263042c0b8cb178c77a9))
* **upload:** deduplicate tags to prevent validation error ([08d2c56](https://github.com/Smana/image-gallery/commit/08d2c562ebf245924bbe54453abccc2969eed678))
* **upload:** deduplicate tags to prevent validation error ([c628ea8](https://github.com/Smana/image-gallery/commit/c628ea8896c0b047863c9f9cc8fcdb965f7df295))
* **upload:** improve error handling and reduce cyclomatic complexity ([40f3d9b](https://github.com/Smana/image-gallery/commit/40f3d9b1514057479f27c020fa48e24874f9991c))

## [1.6.1](https://github.com/Smana/image-gallery/compare/v1.6.0...v1.6.1) (2025-11-01)


### Bug Fixes

* **main:** bug in defining default user-id ([7bc8def](https://github.com/Smana/image-gallery/commit/7bc8def460b1402351e5b0b66fd63286daa5cf00))
* **main:** bug in defining default user-id ([a688bbc](https://github.com/Smana/image-gallery/commit/a688bbc73ebf3e6b5816f1dbd7fe25a8213b1498))

## [1.6.0](https://github.com/Smana/image-gallery/compare/v1.5.3...v1.6.0) (2025-11-01)


### Features

* add user settings and upload handler with tag filtering system ([33a4aa7](https://github.com/Smana/image-gallery/commit/33a4aa779ce9641c6233f999936794836acfaf32))
* add user settings and upload handler with tag filtering system ([5f9d9a2](https://github.com/Smana/image-gallery/commit/5f9d9a227e92b549ee6bb0065030601789969854))


### Bug Fixes

* **tests:** correct sort field to uploaded_at in mock expectation ([23dbc13](https://github.com/Smana/image-gallery/commit/23dbc1357332febd1a3a43c62c47b43267906f67))
* **tests:** remove duplicate GetByTags mock call ([ce9d6c6](https://github.com/Smana/image-gallery/commit/ce9d6c638ec75d8519ada1fa53e889a0a57aeb4b))
* **tests:** update mock expectation for GetWithTags ([73e6367](https://github.com/Smana/image-gallery/commit/73e6367204a28b5da684c1d687232f72a4981c7b))

## [1.5.3](https://github.com/Smana/image-gallery/compare/v1.5.2...v1.5.3) (2025-10-31)


### Bug Fixes

* **observability:** add a test endpoint for database ([076ac68](https://github.com/Smana/image-gallery/commit/076ac68db14c0a1ce73e8b3684a55dd1bcec786c))
* **observability:** add a test endpoint for database ([59e2ea1](https://github.com/Smana/image-gallery/commit/59e2ea1decfc80eaa49f8bb359a4defae1079b63))

## [1.5.2](https://github.com/Smana/image-gallery/compare/v1.5.1...v1.5.2) (2025-10-31)


### Bug Fixes

* **observability:** trigger a releases for the previous change ([fdef7cd](https://github.com/Smana/image-gallery/commit/fdef7cd8b396dea91955a60b10d0eabc2e52a754))

## [1.5.1](https://github.com/Smana/image-gallery/compare/v1.5.0...v1.5.1) (2025-10-31)


### Bug Fixes

* **otel:** add missing trace sampler configuration ([d6ec07e](https://github.com/Smana/image-gallery/commit/d6ec07e178de9f3584113826c8af9d59ddf97d8e))
* **otel:** add missing trace sampler configuration ([e6a6f91](https://github.com/Smana/image-gallery/commit/e6a6f914279f472963c1a0d3095a3fd1cc6f1e1e))

## [1.5.0](https://github.com/Smana/image-gallery/compare/v1.4.2...v1.5.0) (2025-10-31)


### Features

* **observability:** add exemplars, exponential histograms, and configurable sampling ([9f20831](https://github.com/Smana/image-gallery/commit/9f20831d8287d26a100a560da1ada3f23cbb793a))
* **observability:** add exemplars, exponential histograms, and configurable sampling ([04e46e2](https://github.com/Smana/image-gallery/commit/04e46e2e1d93d2bd4fcd52f3f1fb25cd1702e5c1))

## [1.4.2](https://github.com/Smana/image-gallery/compare/v1.4.1...v1.4.2) (2025-10-30)


### Bug Fixes

* **observability:** remove WithInsecure() when using WithEndpointURL ([68a9b11](https://github.com/Smana/image-gallery/commit/68a9b117876935ba5c6cf22158457572eb7dbeda))
* **observability:** remove WithInsecure() when using WithEndpointURL ([130b51f](https://github.com/Smana/image-gallery/commit/130b51f69ffa6663e962a6a7d8e042ebce1b3fb3))

## [1.4.1](https://github.com/Smana/image-gallery/compare/v1.4.0...v1.4.1) (2025-10-29)


### Bug Fixes

* **observability:** correct OTLP endpoint URL handling ([9e402fa](https://github.com/Smana/image-gallery/commit/9e402fa56e3b4cfb24fe22ba0669c0e28a4246f2))
* **observability:** use WithEndpointURL for OTLP exporters ([78b4c46](https://github.com/Smana/image-gallery/commit/78b4c46c0b9c6bc606f06e157b16b4342c88c357))

## [1.4.0](https://github.com/Smana/image-gallery/compare/v1.3.0...v1.4.0) (2025-10-29)


### Features

* **observability:** add comprehensive OpenTelemetry instrumentation ([c7df893](https://github.com/Smana/image-gallery/commit/c7df893208798bb31b5bb6dca0bcc0daf4d2a501))
* **observability:** add comprehensive OpenTelemetry instrumentation ([6f2f695](https://github.com/Smana/image-gallery/commit/6f2f695ee14a327c0c1e21e1e3419186772f81e3))


### Bug Fixes

* **observability:** resolve linting issues - errcheck, gofmt, and cyclomatic complexity ([c77afd5](https://github.com/Smana/image-gallery/commit/c77afd56aeb5a5174307669f08256fb2b84cd496))

## [1.3.0](https://github.com/Smana/image-gallery/compare/v1.2.0...v1.3.0) (2025-10-12)


### Features

* **storage:** support EKS Pod Identity and IAM roles for S3 access ([4873b10](https://github.com/Smana/image-gallery/commit/4873b10dd10676139de7f5854ba4bec30ecc6675))
* **storage:** support EKS Pod Identity and IAM roles for S3 access ([a43960a](https://github.com/Smana/image-gallery/commit/a43960af5b0cc76ba5b72631d566920112241d56))

## [1.2.0](https://github.com/Smana/image-gallery/compare/v1.1.0...v1.2.0) (2025-10-12)


### Features

* add healthchecks handlers ([20059a3](https://github.com/Smana/image-gallery/commit/20059a3e4ba7ece4fe34ffd1a3c65bbdc5030ba1))
* add healthchecks handlers ([f424924](https://github.com/Smana/image-gallery/commit/f424924acc57847b701a6274341be71144d20e17))

## [1.1.0](https://github.com/Smana/image-gallery/compare/v1.0.5...v1.1.0) (2025-09-27)


### Features

* **atlas:** first configuration with atlas local env ([fb2e9f0](https://github.com/Smana/image-gallery/commit/fb2e9f0e86d419a97e2f0af9d71399d6ccc77aeb))
* **atlas:** first configuration with atlas local env ([0324b9c](https://github.com/Smana/image-gallery/commit/0324b9cc6a2939dd3307a75f5869925636209268))

## [1.0.5](https://github.com/Smana/image-gallery/compare/v1.0.4...v1.0.5) (2025-09-14)


### Bug Fixes

* **ci:** remove redundant security scanning ([9d1517a](https://github.com/Smana/image-gallery/commit/9d1517a1e1b7d7dd829cc9d50816f02fc0d7d618))

## [1.0.4](https://github.com/Smana/image-gallery/compare/v1.0.3...v1.0.4) (2025-09-14)


### Bug Fixes

* **ci:** add category in trivy steps ([faa7b08](https://github.com/Smana/image-gallery/commit/faa7b080ac4c0360e7649579e73524c92ebc6d8f))
* **ci:** add category in trivy steps ([cdea3dd](https://github.com/Smana/image-gallery/commit/cdea3dd747e15d9826bbbb527d0b57be90f18645))
* **ci:** integrate trivy with goreleaser ([33e9ce2](https://github.com/Smana/image-gallery/commit/33e9ce211485dfc380ec0e6526bd4b1e52edb079))
* **ci:** integrate trivy with goreleaser ([418c430](https://github.com/Smana/image-gallery/commit/418c430654bb9aedb16b9439921f8599b33854b1))
* **ci:** remove useless steps for building images ([248eabd](https://github.com/Smana/image-gallery/commit/248eabdc36e159b6754236103723d7808e01e080))
* **ci:** replace dagger module with official action ([c07e7e7](https://github.com/Smana/image-gallery/commit/c07e7e71d3a0dc6a40d6770829241afc07e497d4))
* **ci:** replace dagger module with official action ([bd3f834](https://github.com/Smana/image-gallery/commit/bd3f834ffb55a32eaf79bf2f3a837c5b06704147))
* **ci:** use the same hash for both goreleaser and trivy ([7020267](https://github.com/Smana/image-gallery/commit/7020267264b6f62f3daae4176a25259117d435f7))
* **ci:** use the same hash for both goreleaser and trivy ([3787b4e](https://github.com/Smana/image-gallery/commit/3787b4ed82200fce8f3483f2f45463176c072264))

## [1.0.3](https://github.com/Smana/image-gallery/compare/v1.0.2...v1.0.3) (2025-09-14)


### Bug Fixes

* **ci:** docker image to lower ([446e88b](https://github.com/Smana/image-gallery/commit/446e88bdc3c3565ba65fd867f6aa52fd2d5934b9))
* **ci:** docker image to lower ([52698d6](https://github.com/Smana/image-gallery/commit/52698d65f9f1f53c6f3f334a61ca659b34bb97f6))

## [1.0.2](https://github.com/Smana/image-gallery/compare/v1.0.1...v1.0.2) (2025-09-14)


### Bug Fixes

* **ci:** use official github action ([c23ea4b](https://github.com/Smana/image-gallery/commit/c23ea4b9c2a7f62dcd2b209e78dc3d7ac5113f81))
* **ci:** use official github action ([8c254f4](https://github.com/Smana/image-gallery/commit/8c254f467c873298892365bd6388b8790e39ff8c))

## [1.0.1](https://github.com/Smana/image-gallery/compare/v1.0.0...v1.0.1) (2025-09-14)


### Bug Fixes

* **ci:** gorelease workflow ([6bb2786](https://github.com/Smana/image-gallery/commit/6bb2786637785cb49ee4a0d7e778e2855a902202))
* **ci:** gorelease workflow ([55ad87e](https://github.com/Smana/image-gallery/commit/55ad87e162018b7c71e08c50b984e0d62b2d7cd4))

## 1.0.0 (2025-09-14)


### Features

* add comprehensive configuration validation and enhanced setting… ([621107a](https://github.com/Smana/image-gallery/commit/621107aaf6c8ae5715fd187918f0e8a085886e96))
* add comprehensive configuration validation and enhanced settings management ([ae1912f](https://github.com/Smana/image-gallery/commit/ae1912fb601d039a1ee977bade1db929c4d1df8d))
* add comprehensive database layer testing with mocks and integration tests ([be96c99](https://github.com/Smana/image-gallery/commit/be96c992f76623a9928b357fd5c7b42df5e5c436))
* add valkey support ([c95011c](https://github.com/Smana/image-gallery/commit/c95011c7ba38f9c698a60caa1092919934ccb8ed))
* add valkey support ([537d44f](https://github.com/Smana/image-gallery/commit/537d44f7551e02c6f2adfbd7dc72a287a5a77655))
* **aws:** being able to use EKS pod Identity ([9321253](https://github.com/Smana/image-gallery/commit/93212539462992abc6fc29844ee24eec1e0e818c))
* **ci:** configure release-please ([fb827b2](https://github.com/Smana/image-gallery/commit/fb827b2aba2101787d088d0dd01b4be7c894b110))
* **ci:** integrate GoReleaser for standardized build and release process ([6662b0e](https://github.com/Smana/image-gallery/commit/6662b0e6802cb2475859fd3715f7ed116e91a807))
* **ci:** use dagger for ci steps ([3a421a9](https://github.com/Smana/image-gallery/commit/3a421a93eda8271a2cc7e2e2a250b59a5f8ad195))
* enhance storage service with object listing capabilities ([053c2c4](https://github.com/Smana/image-gallery/commit/053c2c48c19719470052670790650a6dafd2a7cf))
* implement complete image gallery with viewing capabilities ([677058f](https://github.com/Smana/image-gallery/commit/677058f4360ee791c9ed98300a38d844e7de3fa7))
* implement comprehensive domain layer with business logic, validation, and events ([56a2ea9](https://github.com/Smana/image-gallery/commit/56a2ea9f2a180013aa1848cb1ff8e5c94c28c016))
* implement comprehensive storage layer with MinIO integration ([00eb2f3](https://github.com/Smana/image-gallery/commit/00eb2f36a90297b3068eaef3fff933a73bfa8d9b))
* implement comprehensive testcontainers integration with PostgreSQL and MinIO ([df354b9](https://github.com/Smana/image-gallery/commit/df354b9953f28a1401a0b383bf404d0a582203c0))
* implement dependency injection container with service interfaces ([f9c78e2](https://github.com/Smana/image-gallery/commit/f9c78e273dc4edec859b47b716f0e5f149b1fb95))
* implement TDD repository pattern with comprehensive unit testing ([cceacae](https://github.com/Smana/image-gallery/commit/cceacae41e14f88f2cd7464eea9486a7df29e1a8))
* integrate Atlas database schema management system ([1f8107f](https://github.com/Smana/image-gallery/commit/1f8107fd9a70dab6ee62facbad97ac0ab9758dd0))
* modernize project structure following Go 2025 best practices ([17ae35b](https://github.com/Smana/image-gallery/commit/17ae35b2863b6a031a4bb2c0835cba741bdf9af8))
* modernize project structure following Go 2025 best practices ([a366cee](https://github.com/Smana/image-gallery/commit/a366cee3d59cab3dc07e593854b271dc02bcc2af))
* **release:** coordinate release-please versions with Docker image tags ([925e6f4](https://github.com/Smana/image-gallery/commit/925e6f4fd1ffde76975d303381a3581f72f25b93))


### Bug Fixes

* add missing application service to docker-compose ([b9c2f39](https://github.com/Smana/image-gallery/commit/b9c2f3915afd859b45abf04ee318a3f92238bcb1))
* **ci:** add missing --platform flag to dagger with-platform commands ([35e28aa](https://github.com/Smana/image-gallery/commit/35e28aaedd5ef757d8d47fb27cf30f38b4239097))
* **ci:** correct Dagger module syntax for build command ([01b090e](https://github.com/Smana/image-gallery/commit/01b090edce2729bb9323627add0f8f061c922518))
* **ci:** correct package path for binary build ([a2a0191](https://github.com/Smana/image-gallery/commit/a2a0191e67ec383b4027e2144040ada6542ba549))
* **ci:** resolve shell syntax errors in build-push workflow ([50b3e80](https://github.com/Smana/image-gallery/commit/50b3e80e825d08f14ccf6799dd6bede097290fef))
* **ci:** resolve shell syntax errors in dagger build steps ([53e5f7a](https://github.com/Smana/image-gallery/commit/53e5f7a188b0f797279b484fe1b8439ce391a53c))
* **ci:** use full module path for Go package build ([ccc72d4](https://github.com/Smana/image-gallery/commit/ccc72d410ecb48d1570de5911a993a44966e7d7c))
* downgrade Go version and clean up dependencies ([cf6cf99](https://github.com/Smana/image-gallery/commit/cf6cf99d5ce759a9b9bf6110590728f7be99cc4d))
* update Docker configuration for Go 1.24 compatibility ([2402410](https://github.com/Smana/image-gallery/commit/2402410b3b0a20ec19d19578b9ceaa827e2ea964))


### Documentation

* comprehensive cleanup and README update ([9f93283](https://github.com/Smana/image-gallery/commit/9f9328386005854040dadc0040e494fbfa91479f))
* refactor structure ([5077dc8](https://github.com/Smana/image-gallery/commit/5077dc8e4cd6481a5199ffc77559af5c3206d8bf))


### Code Refactoring

* enhance project structure following golang-standards layout ([cbf7d1b](https://github.com/Smana/image-gallery/commit/cbf7d1b35afdc92b0889f8882d58541028f4be3b))
* rename repository from golang-helloworld to image-gallery ([27f0be9](https://github.com/Smana/image-gallery/commit/27f0be92a9d6f41369a1dbf3ef91c11f23eac598))

## [Unreleased]

### Added
- Initial project setup with Go 1.25
- Clean architecture implementation with domain-driven design
- RESTful API for image management
- PostgreSQL database with Atlas migrations
- S3-compatible storage (MinIO/AWS) with EKS Pod Identity support
- Valkey (Redis-compatible) caching layer
- Comprehensive testing with testcontainers
- Dagger-based CI/CD pipeline using community modules
- Security scanning with Trivy and govulncheck
- Multi-platform binary builds (Linux, macOS, Windows)
- Multi-architecture container images (AMD64, ARM64)
- Split GitHub Actions workflows (CI validation and build/push)
- Release Please integration for automated releases
- Conventional commits validation
- Comprehensive documentation structure

### Security
- Container vulnerability scanning with Trivy
- Go dependency vulnerability scanning with govulncheck
- Distroless container base images
- SARIF security report uploads to GitHub Security tab

### Documentation
- Complete development setup guide
- Architecture documentation with mermaid diagrams
- Security practices and incident response guide
- CI/CD pipeline documentation
- Release process with conventional commits

---

**Note**: This changelog will be automatically maintained by Release Please based on conventional commit messages starting with the first release.

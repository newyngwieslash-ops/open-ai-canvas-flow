import { spawnSync } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const projectRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const commands = [
    ["bun", ["test", "test/fisher-h3-video.test.ts", "test/fisher-h3-api.test.ts", "test/fisher-h3-defaults.test.ts", "test/fisher-h3-relay.test.ts", "test/fisher-h3-config.test.ts", "test/fisher-h3-canvas-inputs.test.ts", "test/fisher-h3-ui-state.test.ts", "test/fisher-h3-controls.test.tsx", "test/video-api-contracts.test.ts", "test/canvas-video-frame.test.ts", "test/video-generation-options.test.ts", "test/canvas-generation-submission.test.ts", "test/model-capabilities.test.ts", "test/canvas-node-generation-mentions.test.ts", "test/creation-generation-config.test.ts"], "web"],
    ["go", ["test", "./internal/app", "./internal/handler", "./internal/protocol", "./internal/generation", "./cmd/server", "-run", "TestFisher|TestProviderRequest|TestPostJSON|TestAuthorizeSystemProxy|TestAuthorizeCustomRelay|TestCustomRelay|TestSystemProxy|TestBillingFailure|TestBeginTaskRoute|TestDeclarativeTextStreaming|TestMiniMaxVideoFailureMessages|TestOfficialVideoProfilesPreserveExplicitMediaRoles|TestOfficialProtocolPackagesAreSelfContainedDeclarativePlugins|TestAllowedOrigin|TestParseCORSPolicy", "-count=1"], "backend"],
];

for (const [program, args, cwd] of commands) {
    console.log(`${program} ${args.join(" ")}`);
    const result = spawnSync(program, args, { cwd: resolve(projectRoot, cwd), stdio: "inherit" });
    if (result.error) throw result.error;
    if (result.status !== 0) process.exit(result.status ?? 1);
}

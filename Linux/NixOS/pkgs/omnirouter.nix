{
  lib,
  buildNpmPackage,
  fetchFromGitHub,
  python3,
  pkg-config,
  libsecret,
  libx11,
  stdenv,
  nodejs_22,
  python311,
  vips,
  gnumake,
  gcc,
  perl,
}:

let
  nodejs = nodejs_22;
  buildNpmPackage' = buildNpmPackage.override { inherit nodejs; };
in
buildNpmPackage' rec {
  pname = "omnirouter";
  version = "3.8.50";

  src = fetchFromGitHub {
    owner = "diegosouzapw";
    repo = "OmniRoute";
    rev = "v3.8.50";
    hash = "sha256-+2FMc9wrvPtQS3+mGsBVvKrd5RprYe/r/GJvjAVMBpc=";
  };

  npmDepsFetcherVersion = 2;
  npmDepsHash = "sha256-wa5vQMYugA8E7lXOh4lgNH7JNbKOinNaW6Zk8Mw2e8k=";

  nativeBuildInputs = [
    python311
    python3
    pkg-config
    nodejs
    gnumake
    gcc
    perl
  ];

  buildInputs = [
    libsecret
    vips
  ]
  ++ lib.optionals stdenv.hostPlatform.isLinux [
    libx11
  ];

  env = {
    NEXT_TELEMETRY_DISABLED = "1";
    # Turbopack's native memory use exceeds GitHub-hosted runner capacity on
    # current OmniRouter releases. Webpack is the upstream-supported fallback.
    OMNIROUTE_USE_TURBOPACK = "0";
    # v3.8.50 can exhaust a 6 GiB V8 heap during the Webpack production pass.
    # Keep the upstream-supported explicit heap setting below the
    # GitHub-hosted runner's total memory limit.
    OMNIROUTE_BUILD_MEMORY_MB = "6656";
    npm_config_arch = stdenv.hostPlatform.parsed.cpu.name;
    SHARP_IGNORE_GLOBAL_LIBVIPS = "0";
    ONNXRUNTIME_NODE_INSTALL = "skip";
    ONNXRUNTIME_NODE_INSTALL_CUDA = "skip";
  };

  npmRebuildFlags = [ "--ignore-scripts" ];

  doCheck = false;

  postPatch = ''
    if [ -f src/app/layout.tsx ]; then
      ${perl}/bin/perl -i -0pe '
        s|import \{ Inter \} from "next/font/google";\n?||g;
        s|const inter = Inter\(\{[^}]*\}\);|const inter = { variable: "--font-inter", className: "" };|gs;
      ' src/app/layout.tsx
    fi
  '';

  buildPhase = ''
    export NODE_ENV=production
    export CYPRESS_INSTALL_BINARY=0
    export HUSKY_SKIP_INSTALL=1
    npm rebuild better-sqlite3 --build-from-source
    npm run build

    # Next.js preserves this npm bin link as an absolute build-time path.
    semverBin=.build/next/standalone/node_modules/global-agent/node_modules/.bin/semver
    if [ -L "$semverBin" ]; then
      ln -sfn ../semver/bin/semver.js "$semverBin"
    fi
  '';

  installPhase = ''
    runHook preInstall

    mkdir -p $out/share/omnirouter
    cp -a . $out/share/omnirouter/
    rm -f $out/share/omnirouter/.env

    mkdir -p $out/bin
    cat > $out/bin/omnirouter <<EOF
    #!/bin/sh
    cd $out/share/omnirouter/.build/next/standalone
    export DATA_DIR="\''${DATA_DIR:-\''${HOME:-/var/lib/omnirouter}}"
    export APP_LOG_FILE_PATH="\''${APP_LOG_FILE_PATH:-\''${DATA_DIR}/logs/application/app.log}"
    export NODE_ENV=production
    export OMNIROUTE_NO_UPDATE_NOTIFIER="\''${OMNIROUTE_NO_UPDATE_NOTIFIER:-1}"
    exec ${lib.getExe nodejs} dev/run-standalone.mjs "\$@"
    EOF

    chmod +x $out/bin/omnirouter

    cat > $out/bin/omniroute <<EOF
    #!/bin/sh
    cd $out/share/omnirouter
    export DATA_DIR="\''${DATA_DIR:-\''${HOME:-/var/lib/omnirouter}}"
    export NODE_ENV=production
    export OMNIROUTE_NO_UPDATE_NOTIFIER="\''${OMNIROUTE_NO_UPDATE_NOTIFIER:-1}"
    exec ${lib.getExe nodejs} bin/omniroute.mjs "\$@"
    EOF

    chmod +x $out/bin/omniroute

    runHook postInstall
  '';

  meta = with lib; {
    description = "OmniRouter: A versatile LLM router";
    homepage = "https://github.com/diegosouzapw/OmniRoute";
    license = licenses.mit;
  };
}

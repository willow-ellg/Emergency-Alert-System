{
  pkgs ? import <nixpkgs> { },
}:
pkgs.mkShell {
  packages = with pkgs; [
    go
    gcc
    gnumake
    curl
    jq
  ];

  shellHook = ''
    export CGO_ENABLED=0
    echo "Emergency Alert System development shell"
    echo "  go:       $(go version)"
    echo "  make:     $(make --version | head -n1)"
    echo ""
    echo "  make run       start the full hub on :8080 (token: dev-token)"
    echo "  make snapshot  render the standalone emergency snapshot"
    echo "  make beacon    serve the thin snapshot on :8081"
    echo "  make test      run the test suite"
  '';
}

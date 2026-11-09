{
  pkgs ? import <nixpkgs> { },
}:
pkgs.buildGoModule {
  pname = "emergency-alert-system";
  version = "0.1.0";
  src = ./.;
  vendorHash = null;
  subPackages = [
    "cmd/hub"
    "cmd/beacon"
  ];
  env.CGO_ENABLED = 0;
  ldflags = [
    "-s"
    "-w"
  ];
}

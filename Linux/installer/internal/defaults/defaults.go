package defaults

const ShaCryptRounds = 656000

const StagingTempPattern = "wahrwelt-nixos-*"

var ExtraSubstituters = []string{
	"https://nix-community.cachix.org",
	"https://hyprland.cachix.org",
	"https://quickshell.cachix.org",
	"https://numtide.cachix.org",
	"https://wahrwelt.cachix.org",
}

var ExtraTrustedPublicKeys = []string{
	"nix-community.cachix.org-1:mB9FSh9qf2dCimDSUo8Zy7bkq5CX+/rkCWyvRCYg3Fs=",
	"hyprland.cachix.org-1:a7pgxzMz7+chwVL3/pzj6jIBMioiJM7ypFP8PwtkuGc=",
	"quickshell.cachix.org-1:tjWMR3PQd01gN6YtjSRUdHHHUgrSLFIgwqrCQjFXVOU=",
	"numtide.cachix.org-1:2ps1kLBUWjxIneOy1Ik6cQjb41X0iXVXeHigGmycPPE=",
	"wahrwelt.cachix.org-1:WLQwCcRx6rjWf9TRHfuj5NIvTc0M52jTz9uptJlwXhA=",
}

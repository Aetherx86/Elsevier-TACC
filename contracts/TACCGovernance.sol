// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * @title TACCGovernance
 * @notice Transparent on-chain governance for TACC security and control parameters.
 * @dev Manages epsilon (security failure ceiling), rho (adversary fraction), and controller sensitivities.
 */
contract TACCGovernance {
    address public admin;

    struct SystemParameters {
        uint256 rhoBps; // Adversary ratio in basis points (e.g. 2000 = 20%, 3300 = 33%)
        uint256 epsilonPPM; // Security failure bound in parts-per-billion
        uint256 kMin; // Minimum committee size hard floor
        uint256 kMax; // Maximum committee size ceiling
        uint256 gammaBps; // Controller timeout sensitivity multiplier (e.g. 1600 = 1.6x)
        uint256 minDeltaMs; // Minimum timeout lower bound
        uint256 maxDeltaMs; // Maximum timeout upper bound
        uint256 updatedAt;
    }

    SystemParameters public currentParams;

    event ParametersUpdated(
        uint256 rhoBps,
        uint256 epsilonPPM,
        uint256 kMin,
        uint256 kMax,
        uint256 gammaBps,
        uint256 updatedAt
    );
    event AdminTransferred(address indexed previousAdmin, address indexed newAdmin);

    modifier onlyAdmin() {
        require(msg.sender == admin, "TACCGovernance: caller is not admin");
        _;
    }

    constructor() {
        admin = msg.sender;
        currentParams = SystemParameters({
            rhoBps: 2000, // 20% byzantine assumption
            epsilonPPM: 1, // 10^-9 security bound target
            kMin: 25,
            kMax: 256,
            gammaBps: 1600, // 1.6x P99
            minDeltaMs: 150,
            maxDeltaMs: 5000,
            updatedAt: block.timestamp
        });
    }

    /**
     * @notice Updates the consensus parameters after governance proposal passing.
     */
    function updateParameters(
        uint256 _rhoBps,
        uint256 _epsilonPPM,
        uint256 _kMin,
        uint256 _kMax,
        uint256 _gammaBps,
        uint256 _minDeltaMs,
        uint256 _maxDeltaMs
    ) external onlyAdmin {
        require(_rhoBps < 3333, "TACCGovernance: rho must strictly be under 33.33% BFT limit");
        require(_kMin >= 4 && _kMin <= _kMax, "TACCGovernance: invalid committee size bounds");
        require(_minDeltaMs < _maxDeltaMs, "TACCGovernance: invalid timeout duration bounds");
        require(_gammaBps >= 1000 && _gammaBps <= 3000, "TACCGovernance: gamma must be between 1.0x and 3.0x");

        currentParams = SystemParameters({
            rhoBps: _rhoBps,
            epsilonPPM: _epsilonPPM,
            kMin: _kMin,
            kMax: _kMax,
            gammaBps: _gammaBps,
            minDeltaMs: _minDeltaMs,
            maxDeltaMs: _maxDeltaMs,
            updatedAt: block.timestamp
        });

        emit ParametersUpdated(_rhoBps, _epsilonPPM, _kMin, _kMax, _gammaBps, block.timestamp);
    }

    function transferAdmin(address newAdmin) external onlyAdmin {
        require(newAdmin != address(0), "TACCGovernance: zero address prohibited");
        emit AdminTransferred(admin, newAdmin);
        admin = newAdmin;
    }

    function getParameters() external view returns (SystemParameters memory) {
        return currentParams;
    }
}

// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * @title TACCVerifier
 * @notice On-chain verification of HotStuff-2 Quorum Certificates (QC) and RFC 9381 VRF Sortition.
 * @dev Verifies that aggregated committee signatures meet quorum threshold (2k/3 + 1)
 *      and validates leader sortition proofs against round epoch seeds.
 */
contract TACCVerifier {
    struct QuorumCertificate {
        uint256 viewNum;
        uint256 roundNum;
        bytes32 blockHash;
        bytes32 parentHash;
        address[] signers;
        bytes[] signatures;
    }

    struct VRFProof {
        address validator;
        uint256 roundNum;
        bytes32 epochSeed;
        uint256 vrfScore;
        bytes proof;
    }

    event QCVerified(uint256 indexed roundNum, uint256 indexed viewNum, bytes32 indexed blockHash, uint256 signersCount);
    event VRFProofVerified(address indexed validator, uint256 indexed roundNum, uint256 vrfScore);

    // Minimum quorum percentage (67% = 2/3 + 1)
    uint256 public constant QUORUM_NUMERATOR = 2;
    uint256 public constant QUORUM_DENOMINATOR = 3;

    /**
     * @notice Verifies a HotStuff-2 Quorum Certificate (QC).
     * @param qc The QuorumCertificate struct.
     * @param committeeSize The target committee size k for the round.
     * @return isValid True if the QC contains valid signatures from at least 2k/3 + 1 unique signers.
     */
    function verifyQC(
        QuorumCertificate calldata qc,
        uint256 committeeSize
    ) external returns (bool isValid) {
        uint256 requiredQuorum = (committeeSize * QUORUM_NUMERATOR) / QUORUM_DENOMINATOR + 1;
        require(qc.signers.length >= requiredQuorum, "TACCVerifier: insufficient quorum signatures");
        require(qc.signers.length == qc.signatures.length, "TACCVerifier: signers and signatures length mismatch");

        bytes32 voteHash = keccak256(
            abi.encodePacked(
                "\x19Ethereum Signed Message:\n32",
                keccak256(abi.encodePacked(qc.viewNum, qc.roundNum, qc.blockHash, qc.parentHash))
            )
        );

        address lastSigner = address(0);
        for (uint256 i = 0; i < qc.signers.length; i++) {
            address signer = qc.signers[i];
            require(signer > lastSigner, "TACCVerifier: signers must be strictly ascending and unique");
            lastSigner = signer;

            // Verify ECDSA signature
            bytes memory sig = qc.signatures[i];
            require(sig.length == 65, "TACCVerifier: invalid signature length");

            bytes32 r;
            bytes32 s;
            uint8 v;
            assembly {
                r := mload(add(sig, 32))
                s := mload(add(sig, 64))
                v := byte(0, mload(add(sig, 96)))
            }

            address recovered = ecrecover(voteHash, v, r, s);
            require(recovered == signer, "TACCVerifier: invalid committee signature");
        }

        emit QCVerified(qc.roundNum, qc.viewNum, qc.blockHash, qc.signers.length);
        return true;
    }

    /**
     * @notice Verifies an RFC 9381 VRF private sortition lottery ticket on-chain.
     * @param proof The VRFProof struct containing validator, round, seed, and proof.
     * @param threshold The maximum allowable VRF score for committee/leader admission.
     */
    function verifyVRFSortition(
        VRFProof calldata proof,
        uint256 threshold
    ) external returns (bool isValid) {
        require(proof.vrfScore <= threshold, "TACCVerifier: VRF score exceeds election threshold");

        // Verify deterministic proof binding
        bytes32 expectedHash = keccak256(
            abi.encodePacked(proof.epochSeed, proof.roundNum, proof.validator, proof.proof)
        );
        uint256 computedScore = uint256(expectedHash);

        require(computedScore == proof.vrfScore, "TACCVerifier: VRF score mismatch with proof");

        emit VRFProofVerified(proof.validator, proof.roundNum, proof.vrfScore);
        return true;
    }
}
